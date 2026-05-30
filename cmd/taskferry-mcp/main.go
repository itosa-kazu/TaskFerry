package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/itosa-kazu/TaskFerry/internal/localapi"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id,omitempty"`
	Result  any            `json:"result,omitempty"`
	Error   *responseError `json:"error,omitempty"`
}

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func main() {
	server := &mcpServer{
		client: localapi.New(getenv("TASKFERRY_LOCAL_URL", "http://127.0.0.1:4318"), getenv("TASKFERRY_LOCAL_API_TOKEN", "")),
		writer: bufio.NewWriter(os.Stdout),
	}
	scanner := bufio.NewScanner(os.Stdin)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 16*1024*1024)
	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			server.write(response{JSONRPC: "2.0", Error: &responseError{Code: -32700, Message: err.Error()}})
			continue
		}
		server.handle(req)
	}
}

type mcpServer struct {
	client *localapi.Client
	writer *bufio.Writer
}

func (s *mcpServer) handle(req request) {
	switch req.Method {
	case "initialize":
		s.write(response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "taskferry",
				"version": "0.1.0",
			},
		}})
	case "notifications/initialized":
		return
	case "tools/list":
		s.write(response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": tools()}})
	case "tools/call":
		result, err := s.callTool(req.Params)
		if err != nil {
			s.write(response{JSONRPC: "2.0", ID: req.ID, Error: &responseError{Code: -32000, Message: err.Error()}})
			return
		}
		s.write(response{JSONRPC: "2.0", ID: req.ID, Result: result})
	default:
		if req.ID != nil {
			s.write(response{JSONRPC: "2.0", ID: req.ID, Error: &responseError{Code: -32601, Message: "method not found"}})
		}
	}
}

func (s *mcpServer) callTool(params json.RawMessage) (any, error) {
	var in struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &in); err != nil {
		return nil, err
	}
	raw, err := s.runTool(in.Name, in.Arguments)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": string(raw)},
		},
	}, nil
}

func (s *mcpServer) runTool(name string, args map[string]any) (json.RawMessage, error) {
	switch name {
	case "taskferry_health":
		return s.client.Health()
	case "taskferry_register_agent":
		return s.client.CreateAgent(str(args, "handle"), str(args, "display_name"), str(args, "description"), str(args, "tagline"), strList(args, "capabilities"), boolValue(args, "public_profile", false))
	case "taskferry_show_invite":
		return s.client.AgentInvite(str(args, "agent"))
	case "taskferry_add_friend":
		return s.client.FriendAdd(str(args, "from"), str(args, "invite"), str(args, "message"))
	case "taskferry_request_connection":
		return s.client.RequestConnection(str(args, "from"), str(args, "to"), str(args, "message"))
	case "taskferry_accept_connection":
		return s.client.AcceptConnection(str(args, "from"), str(args, "to"))
	case "taskferry_create_task":
		return s.client.CreateTask(str(args, "from"), str(args, "to"), str(args, "title"), str(args, "description"), strList(args, "requirements"), intValue(args, "max_revisions", 3), str(args, "expected_format"))
	case "taskferry_check_inbox":
		return s.client.Inbox(str(args, "agent"), boolValue(args, "unprocessed", true))
	case "taskferry_ack_message":
		return s.client.AckMessage(str(args, "message_id"))
	case "taskferry_list_tasks":
		return s.client.Tasks()
	case "taskferry_accept_task":
		return s.client.AcceptTask(str(args, "task_id"), str(args, "from"), str(args, "message"))
	case "taskferry_decline_task":
		return s.client.DeclineTask(str(args, "task_id"), str(args, "from"), str(args, "reason"))
	case "taskferry_submit_artifact":
		content := args["content"]
		if content == nil {
			content = map[string]any{}
		}
		return s.client.SubmitArtifact(str(args, "task_id"), str(args, "from"), strDefault(args, "artifact_type", "json"), content, str(args, "notes"))
	case "taskferry_request_revision":
		return s.client.RequestRevision(str(args, "task_id"), str(args, "from"), str(args, "reason"), strList(args, "requested_changes"))
	case "taskferry_complete_task":
		return s.client.CompleteTask(str(args, "task_id"), str(args, "from"), str(args, "message"))
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

func (s *mcpServer) write(resp response) {
	raw, _ := json.Marshal(resp)
	_, _ = s.writer.Write(raw)
	_ = s.writer.WriteByte('\n')
	_ = s.writer.Flush()
}

func tools() []tool {
	return []tool{
		{Name: "taskferry_health", Description: "Check the local TaskFerry client health.", InputSchema: object(nil, nil)},
		{Name: "taskferry_register_agent", Description: "Register or update a local TaskFerry agent.", InputSchema: object(map[string]any{"handle": strSchema(), "display_name": strSchema(), "description": strSchema(), "tagline": strSchema(), "capabilities": arraySchema(), "public_profile": boolSchema()}, []string{"handle"})},
		{Name: "taskferry_show_invite", Description: "Show this agent's TaskFerry invite link.", InputSchema: object(map[string]any{"agent": strSchema()}, []string{"agent"})},
		{Name: "taskferry_add_friend", Description: "Request a TaskFerry connection using a taskferry:// invite link.", InputSchema: object(map[string]any{"from": strSchema(), "invite": strSchema(), "message": strSchema()}, []string{"from", "invite"})},
		{Name: "taskferry_request_connection", Description: "Request approval to communicate with another agent.", InputSchema: object(map[string]any{"from": strSchema(), "to": strSchema(), "message": strSchema()}, []string{"from", "to"})},
		{Name: "taskferry_accept_connection", Description: "Accept a connection request from another agent.", InputSchema: object(map[string]any{"from": strSchema(), "to": strSchema()}, []string{"from", "to"})},
		{Name: "taskferry_create_task", Description: "Create a typed TaskFerry task for another agent.", InputSchema: object(map[string]any{"from": strSchema(), "to": strSchema(), "title": strSchema(), "description": strSchema(), "requirements": arraySchema(), "max_revisions": intSchema(), "expected_format": strSchema()}, []string{"from", "to", "title", "description"})},
		{Name: "taskferry_check_inbox", Description: "Read a local agent inbox.", InputSchema: object(map[string]any{"agent": strSchema(), "unprocessed": boolSchema()}, []string{"agent"})},
		{Name: "taskferry_ack_message", Description: "Mark a local inbox message as processed.", InputSchema: object(map[string]any{"message_id": strSchema()}, []string{"message_id"})},
		{Name: "taskferry_list_tasks", Description: "List local TaskFerry tasks.", InputSchema: object(nil, nil)},
		{Name: "taskferry_accept_task", Description: "Accept a task assigned to this agent.", InputSchema: object(map[string]any{"task_id": strSchema(), "from": strSchema(), "message": strSchema()}, []string{"task_id", "from"})},
		{Name: "taskferry_decline_task", Description: "Decline a task assigned to this agent.", InputSchema: object(map[string]any{"task_id": strSchema(), "from": strSchema(), "reason": strSchema()}, []string{"task_id", "from"})},
		{Name: "taskferry_submit_artifact", Description: "Submit an artifact for a TaskFerry task.", InputSchema: object(map[string]any{"task_id": strSchema(), "from": strSchema(), "artifact_type": strSchema(), "content": map[string]any{}, "notes": strSchema()}, []string{"task_id", "from", "content"})},
		{Name: "taskferry_request_revision", Description: "Request revision for a submitted artifact.", InputSchema: object(map[string]any{"task_id": strSchema(), "from": strSchema(), "reason": strSchema(), "requested_changes": arraySchema()}, []string{"task_id", "from", "reason"})},
		{Name: "taskferry_complete_task", Description: "Mark a task as completed after accepting the artifact.", InputSchema: object(map[string]any{"task_id": strSchema(), "from": strSchema(), "message": strSchema()}, []string{"task_id", "from"})},
	}
}

func object(properties map[string]any, required []string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required}
}

func strSchema() map[string]any {
	return map[string]any{"type": "string"}
}

func arraySchema() map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
}

func intSchema() map[string]any {
	return map[string]any{"type": "integer"}
}

func boolSchema() map[string]any {
	return map[string]any{"type": "boolean"}
}

func str(args map[string]any, key string) string {
	return strDefault(args, key, "")
}

func strDefault(args map[string]any, key string, fallback string) string {
	value, ok := args[key]
	if !ok || value == nil {
		return fallback
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}

func strList(args map[string]any, key string) []string {
	value, ok := args[key]
	if !ok || value == nil {
		return nil
	}
	switch v := value.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, fmt.Sprint(item))
		}
		return out
	case []string:
		return v
	case string:
		return localapi.ParseList(v)
	default:
		return nil
	}
}

func intValue(args map[string]any, key string, fallback int) int {
	value, ok := args[key]
	if !ok || value == nil {
		return fallback
	}
	switch v := value.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return fallback
	}
}

func boolValue(args map[string]any, key string, fallback bool) bool {
	value, ok := args[key]
	if !ok || value == nil {
		return fallback
	}
	v, ok := value.(bool)
	if !ok {
		return fallback
	}
	return v
}

func getenv(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
