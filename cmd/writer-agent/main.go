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

func main() {
	baseURL := flag.String("base-url", "http://127.0.0.1:4319", "local client base URL")
	handle := flag.String("handle", "@bob/writer", "agent handle")
	apiToken := flag.String("api-token", getenv("TASKFERRY_LOCAL_API_TOKEN", "AGENTCHAT_LOCAL_API_TOKEN", ""), "local client API token")
	flag.Parse()

	mustPost(*baseURL+"/agents", *apiToken, map[string]any{
		"handle":       *handle,
		"display_name": "Writer Agent",
		"description":  "Writes and revises campaign copy.",
		"capabilities": []string{"copywriting", "revision"},
	})
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		msgs := mustInbox(*baseURL, *handle, *apiToken)
		for _, msg := range msgs {
			switch msg.Type {
			case protocol.MessageTypeConnectionRequest:
				fmt.Printf("accepting connection from %s\n", msg.Sender)
				mustPost(*baseURL+"/connections/accept", *apiToken, map[string]any{"from": *handle, "to": msg.Sender})
				mustAck(*baseURL, msg.MessageID, *apiToken)
			case protocol.MessageTypeTaskRequest:
				fmt.Printf("accepting task %s\n", msg.TaskID)
				mustPost(*baseURL+"/tasks/"+msg.TaskID+"/accept", *apiToken, map[string]any{"from": *handle, "message": "Accepted."})
				mustPost(*baseURL+"/tasks/"+msg.TaskID+"/artifacts", *apiToken, map[string]any{
					"from":          *handle,
					"artifact_type": "json",
					"content": map[string]any{"lines": []string{
						"Meet the hero before midnight.",
						"Your choice rewrites the campaign.",
						"A sealed promise crosses the frontier.",
					}},
					"notes": "First draft.",
				})
				mustAck(*baseURL, msg.MessageID, *apiToken)
			case protocol.MessageTypeRevisionRequest:
				fmt.Printf("submitting revision for task %s\n", msg.TaskID)
				mustPost(*baseURL+"/tasks/"+msg.TaskID+"/artifacts", *apiToken, map[string]any{
					"from":          *handle,
					"artifact_type": "json",
					"content": map[string]any{"lines": []string{
						"Under moonlight, a lost pact calls your name.",
						"Choose once, and the kingdom remembers forever.",
						"Carry the sealed letter beyond the last gate.",
					}},
					"notes": "Second draft after revision request.",
				})
				mustAck(*baseURL, msg.MessageID, *apiToken)
			case protocol.MessageTypeTaskComplete:
				fmt.Printf("task %s completed\n", msg.TaskID)
				mustAck(*baseURL, msg.MessageID, *apiToken)
				return
			default:
				mustAck(*baseURL, msg.MessageID, *apiToken)
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	panic("writer-agent timed out")
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
