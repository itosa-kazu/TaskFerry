package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/itosa-kazu/TaskFerry/internal/protocol"
)

type localMessage struct {
	MessageID string               `json:"MessageID"`
	TaskID    string               `json:"TaskID"`
	Type      protocol.MessageType `json:"Type"`
	Sender    string               `json:"Sender"`
	Plaintext json.RawMessage      `json:"Plaintext"`
}

type inboxResponse struct {
	Messages []localMessage `json:"messages"`
}

type createTaskResponse struct {
	OK     bool   `json:"ok"`
	TaskID string `json:"task_id"`
	Error  string `json:"error"`
}

type artifactPayload struct {
	Version int `json:"version"`
}

func main() {
	baseURL := flag.String("base-url", "http://127.0.0.1:4318", "local client base URL")
	handle := flag.String("handle", "@alice/requester", "agent handle")
	writer := flag.String("writer", "@bob/writer", "writer handle")
	apiToken := flag.String("api-token", getenv("TASKFERRY_LOCAL_API_TOKEN", "AGENTCHAT_LOCAL_API_TOKEN", ""), "local client API token")
	flag.Parse()

	mustPost(*baseURL+"/agents", *apiToken, map[string]any{
		"handle":       *handle,
		"display_name": "Requester Agent",
		"description":  "Creates tasks and reviews submissions.",
		"capabilities": []string{"create_task", "review_artifact"},
	})
	fmt.Printf("requesting connection to %s\n", *writer)
	mustPost(*baseURL+"/connections/request", *apiToken, map[string]any{"from": *handle, "to": *writer, "message": "Please connect for task work."})

	taskID := ""
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		msgs := mustInbox(*baseURL, *handle, *apiToken)
		for _, msg := range msgs {
			switch msg.Type {
			case protocol.MessageTypeConnectionAccept:
				fmt.Printf("connection accepted by %s\n", msg.Sender)
				mustAck(*baseURL, msg.MessageID, *apiToken)
				if taskID == "" {
					taskID = mustCreateTask(*baseURL, *apiToken, *handle, *writer)
					fmt.Printf("created task %s\n", taskID)
				}
			case protocol.MessageTypeTaskAccept:
				fmt.Printf("task %s accepted\n", msg.TaskID)
				mustAck(*baseURL, msg.MessageID, *apiToken)
			case protocol.MessageTypeArtifactSubmit:
				var payload artifactPayload
				_ = json.Unmarshal(msg.Plaintext, &payload)
				fmt.Printf("artifact submitted for %s version=%d\n", msg.TaskID, payload.Version)
				if payload.Version <= 1 {
					mustPost(*baseURL+"/tasks/"+msg.TaskID+"/revision", *apiToken, map[string]any{
						"from":              *handle,
						"reason":            "The first draft is too generic for a fantasy campaign.",
						"requested_changes": []string{"Make the lines more atmospheric", "Add a clearer sense of stakes", "Keep each line concise"},
					})
				} else {
					mustPost(*baseURL+"/tasks/"+msg.TaskID+"/complete", *apiToken, map[string]any{"from": *handle, "message": "Accepted."})
					mustAck(*baseURL, msg.MessageID, *apiToken)
					fmt.Printf("completed task %s\n", msg.TaskID)
					return
				}
				mustAck(*baseURL, msg.MessageID, *apiToken)
			default:
				mustAck(*baseURL, msg.MessageID, *apiToken)
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	panic("requester-agent timed out")
}

func mustCreateTask(baseURL string, token string, from string, to string) string {
	raw, _ := json.Marshal(map[string]any{
		"from":          from,
		"to":            to,
		"title":         "Write three concise fantasy RPG ad lines",
		"description":   "Need atmospheric campaign copy for a fictional fantasy game.",
		"requirements":  []string{"Natural English", "A sense of fate", "Under 60 characters per line"},
		"max_revisions": 3,
	})
	req, err := http.NewRequest(http.MethodPost, baseURL+"/tasks", bytes.NewReader(raw))
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "application/json")
	setAuth(req, token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		panic(string(body))
	}
	var out createTaskResponse
	if err := json.Unmarshal(body, &out); err != nil {
		panic(err)
	}
	if !out.OK {
		panic(out.Error)
	}
	return out.TaskID
}

func mustInbox(baseURL string, handle string, token string) []localMessage {
	endpoint := baseURL + "/inbox?agent_id=" + url.QueryEscape(handle) + "&unprocessed=true"
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		panic(err)
	}
	setAuth(req, token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		panic(string(body))
	}
	var out inboxResponse
	if err := json.Unmarshal(body, &out); err != nil {
		panic(err)
	}
	return out.Messages
}

func mustPost(endpoint string, token string, payload any) {
	raw, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "application/json")
	setAuth(req, token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		panic(fmt.Sprintf("%s: %s", endpoint, string(body)))
	}
}

func mustAck(baseURL string, messageID string, token string) {
	mustPost(baseURL+"/messages/"+messageID+"/ack", token, map[string]any{})
}

func setAuth(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func getenv(primary string, legacy string, fallback string) string {
	if value := os.Getenv(primary); value != "" {
		return value
	}
	if value := os.Getenv(legacy); value != "" {
		return value
	}
	return fallback
}
