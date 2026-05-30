package localapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

func New(baseURL string, token string) *Client {
	if baseURL == "" {
		baseURL = "http://127.0.0.1:4318"
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *Client) Health() (json.RawMessage, error) {
	return c.do(http.MethodGet, "/health", nil)
}

func (c *Client) CreateAgent(handle string, displayName string, description string, tagline string, capabilities []string, publicProfile bool) (json.RawMessage, error) {
	return c.do(http.MethodPost, "/agents", map[string]any{
		"handle":         handle,
		"display_name":   displayName,
		"description":    description,
		"tagline":        tagline,
		"capabilities":   capabilities,
		"public_profile": publicProfile,
	})
}

func (c *Client) AgentInvite(agent string) (json.RawMessage, error) {
	q := url.Values{}
	q.Set("agent", agent)
	return c.do(http.MethodGet, "/invites?"+q.Encode(), nil)
}

func (c *Client) FriendAdd(from string, invite string, message string) (json.RawMessage, error) {
	return c.do(http.MethodPost, "/friends/request", map[string]any{
		"from":    from,
		"invite":  invite,
		"message": message,
	})
}

func (c *Client) RequestConnection(from string, to string, message string) (json.RawMessage, error) {
	return c.do(http.MethodPost, "/connections/request", map[string]any{
		"from":    from,
		"to":      to,
		"message": message,
	})
}

func (c *Client) AcceptConnection(from string, to string) (json.RawMessage, error) {
	return c.do(http.MethodPost, "/connections/accept", map[string]any{
		"from": from,
		"to":   to,
	})
}

func (c *Client) CreateTask(from string, to string, title string, description string, requirements []string, maxRevisions int, expectedFormat string) (json.RawMessage, error) {
	return c.do(http.MethodPost, "/tasks", map[string]any{
		"from":            from,
		"to":              to,
		"title":           title,
		"description":     description,
		"requirements":    requirements,
		"max_revisions":   maxRevisions,
		"expected_format": expectedFormat,
	})
}

func (c *Client) Inbox(agentID string, unprocessed bool) (json.RawMessage, error) {
	q := url.Values{}
	q.Set("agent_id", agentID)
	if unprocessed {
		q.Set("unprocessed", "true")
	}
	return c.do(http.MethodGet, "/inbox?"+q.Encode(), nil)
}

func (c *Client) AckMessage(messageID string) (json.RawMessage, error) {
	return c.do(http.MethodPost, "/messages/"+url.PathEscape(messageID)+"/ack", map[string]any{})
}

func (c *Client) Tasks() (json.RawMessage, error) {
	return c.do(http.MethodGet, "/tasks", nil)
}

func (c *Client) AcceptTask(taskID string, from string, message string) (json.RawMessage, error) {
	return c.taskAction(taskID, "accept", map[string]any{"from": from, "message": message})
}

func (c *Client) DeclineTask(taskID string, from string, reason string) (json.RawMessage, error) {
	return c.taskAction(taskID, "decline", map[string]any{"from": from, "reason": reason})
}

func (c *Client) SubmitArtifact(taskID string, from string, artifactType string, content any, notes string) (json.RawMessage, error) {
	return c.taskAction(taskID, "artifacts", map[string]any{
		"from":          from,
		"artifact_type": artifactType,
		"content":       content,
		"notes":         notes,
	})
}

func (c *Client) RequestRevision(taskID string, from string, reason string, requestedChanges []string) (json.RawMessage, error) {
	return c.taskAction(taskID, "revision", map[string]any{
		"from":              from,
		"reason":            reason,
		"requested_changes": requestedChanges,
	})
}

func (c *Client) CompleteTask(taskID string, from string, message string) (json.RawMessage, error) {
	return c.taskAction(taskID, "complete", map[string]any{"from": from, "message": message})
}

func (c *Client) taskAction(taskID string, action string, payload any) (json.RawMessage, error) {
	return c.do(http.MethodPost, "/tasks/"+url.PathEscape(taskID)+"/"+action, payload)
}

func (c *Client) do(method string, path string, payload any) (json.RawMessage, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("taskferry local API status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if len(raw) == 0 {
		return json.RawMessage("{}"), nil
	}
	return json.RawMessage(raw), nil
}

func ParseList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func DecodeJSONValue(raw string) (any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	var out any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	return out, nil
}
