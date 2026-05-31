package client

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/itosa-kazu/TaskFerry/internal/protocol"

	"github.com/gorilla/websocket"
)

type Config struct {
	Addr          string
	ClientID      string
	DeviceID      string
	OwnerID       string
	RelayHTTP     string
	RelayWS       string
	RelayToken    string
	LocalAPIToken string
}

type Server struct {
	cfg            Config
	store          *Store
	httpClient     *http.Client
	relaySend      chan protocol.RelayFrame
	relayReset     chan struct{}
	relayConnected bool
	mu             sync.RWMutex
}

func NewServer(cfg Config, store *Store) *Server {
	return &Server{
		cfg:        cfg,
		store:      store,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		relaySend:  make(chan protocol.RelayFrame, 512),
		relayReset: make(chan struct{}, 1),
	}
}

func (s *Server) StartRelayLoop() {
	go s.relayLoop()
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /", s.handleDashboard)
	mux.HandleFunc("GET /agents", s.handleAgents)
	mux.HandleFunc("POST /agents", s.handleCreateAgent)
	mux.HandleFunc("GET /setup", s.handleSetupPage)
	mux.HandleFunc("POST /setup", s.handleSetupPage)
	mux.HandleFunc("GET /invites", s.handleInviteForAgent)
	mux.HandleFunc("GET /connect", s.handleConnectPage)
	mux.HandleFunc("POST /connect", s.handleConnectPage)
	mux.HandleFunc("POST /friends/request", s.handleFriendRequest)
	mux.HandleFunc("POST /connections/request", s.handleConnectionRequest)
	mux.HandleFunc("POST /connections/accept", s.handleConnectionAccept)
	mux.HandleFunc("POST /messages/send", s.handleSendMessage)
	mux.HandleFunc("GET /inbox", s.handleInbox)
	mux.HandleFunc("POST /messages/", s.handleMessageAction)
	mux.HandleFunc("GET /tasks", s.handleTasksPage)
	mux.HandleFunc("POST /tasks", s.handleCreateTask)
	mux.HandleFunc("POST /tasks/", s.handleTaskAction)
	return s.withCORS(s.withLocalAuth(mux))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	relayConnected := s.relayConnected
	cfg := s.cfg
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "ok",
		"client_id":       cfg.ClientID,
		"device_id":       cfg.DeviceID,
		"relay_connected": relayConnected,
	})
}

func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	var req protocol.CreateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse("invalid_json"))
		return
	}
	cfg := s.config()
	ownerID := req.OwnerID
	if ownerID == "" {
		ownerID = cfg.OwnerID
	}
	rec, err := s.store.CreateAgent(req.Handle, ownerID, cfg.DeviceID, req.DisplayName, req.Description, req.Tagline, req.Capabilities, req.PublicProfile)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse(err.Error()))
		return
	}
	if err := s.registerAgent(rec); err != nil {
		s.store.Log("warn", "relay_register_failed", err.Error(), map[string]string{"handle": rec.Handle})
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "agent": rec.Profile()})
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && acceptsHTML(r) {
		s.handleDashboard(w, r)
		return
	}
	agents, err := s.store.Agents()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse("agents_failed"))
		return
	}
	var profiles []protocol.AgentProfile
	for _, agent := range agents {
		profiles = append(profiles, agent.Profile())
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": profiles})
}

func (s *Server) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleSetupSubmit(w, r)
		return
	}
	data := s.setupPageData(r, "", nil)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := setupTemplate.Execute(w, data); err != nil {
		log.Printf("setup page render failed: %v", err)
	}
}

func (s *Server) handleSetupSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	token := r.FormValue("token")
	if token == "" {
		token = localTokenFromRequest(r)
	}
	if !s.localRequestAuthorized(r, token) {
		data := s.setupPageData(r, "Local API token is required before setup can change this client.", nil)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_ = setupTemplate.Execute(w, data)
		return
	}

	cfg, err := s.setupConfigFromRequest(r)
	if err != nil {
		data := s.setupPageData(r, err.Error(), nil)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = setupTemplate.Execute(w, data)
		return
	}
	if err := s.store.SaveRelayConfig(cfg); err != nil {
		data := s.setupPageData(r, "Could not save local relay config.", nil)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_ = setupTemplate.Execute(w, data)
		return
	}
	s.applyConfig(cfg)

	handle := strings.TrimSpace(r.FormValue("handle"))
	displayName := strings.TrimSpace(r.FormValue("display_name"))
	tagline := strings.TrimSpace(r.FormValue("tagline"))
	capabilities := parseCSV(r.FormValue("capabilities"))
	publicProfile := r.FormValue("public_profile") == "on"
	var agent AgentRecord
	var invite protocol.InviteResponse
	var warnings []string
	if handle != "" {
		if displayName == "" {
			displayName = handle
		}
		agent, err = s.store.CreateAgent(handle, cfg.OwnerID, cfg.DeviceID, displayName, "", tagline, capabilities, publicProfile)
		if err != nil {
			data := s.setupPageData(r, err.Error(), nil)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_ = setupTemplate.Execute(w, data)
			return
		}
		if err := s.registerAgent(agent); err != nil {
			warnings = append(warnings, "Agent was created locally, but relay registration will retry when the relay connection is ready.")
			s.store.Log("warn", "relay_register_failed", err.Error(), map[string]string{"handle": agent.Handle})
		} else if publicProfile {
			if out, err := s.fetchAgentInvite(agent.Handle); err == nil {
				invite = out
			} else {
				warnings = append(warnings, "Agent was registered, but invite lookup failed. Use invite-show after the relay reconnects.")
			}
		}
	}

	data := s.setupPageData(r, "", warnings)
	data["Saved"] = true
	data["Agent"] = agent
	data["HasAgent"] = agent.Handle != ""
	data["InviteURL"] = invite.InviteURL
	data["WebInviteURL"] = invite.WebInviteURL
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := setupTemplate.Execute(w, data); err != nil {
		log.Printf("setup page render failed: %v", err)
	}
}

func (s *Server) setupConfigFromRequest(r *http.Request) (Config, error) {
	current := s.config()
	cfg := current
	cfg.ClientID = strings.TrimSpace(r.FormValue("client_id"))
	cfg.RelayToken = strings.TrimSpace(r.FormValue("relay_token"))
	cfg.RelayHTTP = strings.TrimRight(strings.TrimSpace(r.FormValue("relay_http")), "/")
	cfg.RelayWS = strings.TrimSpace(r.FormValue("relay_ws"))
	if cfg.ClientID == "" || cfg.RelayToken == "" || cfg.RelayHTTP == "" || cfg.RelayWS == "" {
		return Config{}, errors.New("Relay setup link is missing client_id, relay_token, relay_http, or relay_ws.")
	}
	if _, err := url.ParseRequestURI(cfg.RelayHTTP); err != nil {
		return Config{}, errors.New("Relay HTTP URL is invalid.")
	}
	if _, err := url.ParseRequestURI(cfg.RelayWS); err != nil {
		return Config{}, errors.New("Relay WebSocket URL is invalid.")
	}
	if cfg.DeviceID == "" || cfg.DeviceID == "device_dev" {
		cfg.DeviceID = protocol.NewID("device")
	}
	cfg.OwnerID = cfg.ClientID
	return cfg, nil
}

func (s *Server) setupPageData(r *http.Request, pageError string, warnings []string) map[string]any {
	cfg := s.config()
	token := r.FormValue("token")
	if token == "" {
		token = localTokenFromRequest(r)
	}
	handle := r.FormValue("handle")
	if handle == "" {
		handle = suggestedHandle(r.FormValue("owner_name"), r.FormValue("client_id"))
	}
	publicProfile := true
	if r.Method == http.MethodPost {
		publicProfile = r.FormValue("public_profile") == "on"
	} else if r.FormValue("public") == "false" {
		publicProfile = false
	}
	data := map[string]any{
		"ClientID":        firstNonEmpty(r.FormValue("client_id"), cfg.ClientID),
		"RelayToken":      firstNonEmpty(r.FormValue("relay_token"), cfg.RelayToken),
		"RelayHTTP":       firstNonEmpty(r.FormValue("relay_http"), cfg.RelayHTTP),
		"RelayWS":         firstNonEmpty(r.FormValue("relay_ws"), cfg.RelayWS),
		"Handle":          handle,
		"DisplayName":     firstNonEmpty(r.FormValue("display_name"), r.FormValue("owner_name")),
		"Tagline":         firstNonEmpty(r.FormValue("tagline"), "Available for TaskFerry work."),
		"Capabilities":    firstNonEmpty(r.FormValue("capabilities"), "code,review"),
		"PublicProfile":   publicProfile,
		"Token":           token,
		"NeedsToken":      cfg.LocalAPIToken != "" && !s.localRequestAuthorized(r, token),
		"Error":           pageError,
		"Warnings":        warnings,
		"RelayConfigured": cfg.ClientID != "" && cfg.ClientID != "client_dev",
	}
	return data
}

func (s *Server) handleInviteForAgent(w http.ResponseWriter, r *http.Request) {
	handle := r.URL.Query().Get("agent")
	if handle == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse("missing_agent"))
		return
	}
	invite, err := s.fetchAgentInvite(handle)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, invite)
}

func (s *Server) handleFriendRequest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From    string `json:"from"`
		Invite  string `json:"invite"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse("invalid_json"))
		return
	}
	invite, err := s.resolveInvite(req.Invite)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse(err.Error()))
		return
	}
	if invite.Agent == nil || invite.Agent.Handle == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse("invite_missing_agent"))
		return
	}
	message := req.Message
	if message == "" {
		message = "Please connect for TaskFerry work."
	}
	id, err := s.sendTyped(req.From, []string{invite.Agent.Handle}, protocol.MessageTypeConnectionRequest, map[string]any{"message": message, "invite_code": invite.InviteCode}, "", "")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "to": invite.Agent.Handle, "invite_code": invite.InviteCode, "message_id": id})
}

func (s *Server) handleConnectPage(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleConnectSubmit(w, r)
		return
	}

	inviteRaw := r.URL.Query().Get("invite")
	token := localTokenFromRequest(r)
	data := s.connectPageData(r, inviteRaw, token, "")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := connectTemplate.Execute(w, data); err != nil {
		log.Printf("connect page render failed: %v", err)
	}
}

func (s *Server) handleConnectSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	inviteRaw := r.FormValue("invite")
	token := r.FormValue("token")
	if token == "" {
		token = localTokenFromRequest(r)
	}
	if !s.localRequestAuthorized(r, token) {
		data := s.connectPageData(r, inviteRaw, token, "Local API token is required before an identity can act on this invite.")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_ = connectTemplate.Execute(w, data)
		return
	}

	from := r.FormValue("from")
	message := r.FormValue("message")
	if from == "" {
		data := s.connectPageData(r, inviteRaw, token, "Choose a local agent identity before connecting.")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = connectTemplate.Execute(w, data)
		return
	}
	if message == "" {
		message = "Please connect for TaskFerry work."
	}
	invite, err := s.resolveInvite(inviteRaw)
	if err != nil {
		data := s.connectPageData(r, inviteRaw, token, err.Error())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = connectTemplate.Execute(w, data)
		return
	}
	if invite.Agent == nil || invite.Agent.Handle == "" {
		data := s.connectPageData(r, inviteRaw, token, "Invite does not contain an agent profile.")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = connectTemplate.Execute(w, data)
		return
	}
	msgID, err := s.sendTyped(from, []string{invite.Agent.Handle}, protocol.MessageTypeConnectionRequest, map[string]any{"message": message, "invite_code": invite.InviteCode}, "", "")
	if err != nil {
		data := s.connectPageData(r, inviteRaw, token, err.Error())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = connectTemplate.Execute(w, data)
		return
	}

	data := s.connectPageData(r, inviteRaw, token, "")
	data["Sent"] = true
	data["From"] = from
	data["To"] = invite.Agent.Handle
	data["MessageID"] = msgID
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := connectTemplate.Execute(w, data); err != nil {
		log.Printf("connect page render failed: %v", err)
	}
}

func (s *Server) connectPageData(r *http.Request, inviteRaw string, token string, pageError string) map[string]any {
	cfg := s.config()
	data := map[string]any{
		"InviteRaw":  inviteRaw,
		"Token":      token,
		"NeedsToken": cfg.LocalAPIToken != "" && !s.localRequestAuthorized(r, token),
		"Error":      pageError,
		"Message":    "Please connect for TaskFerry work.",
	}
	if inviteRaw != "" {
		if invite, err := s.resolveInvite(inviteRaw); err == nil {
			data["Invite"] = invite
			data["InviteAgent"] = invite.Agent
		} else if pageError == "" {
			data["Error"] = err.Error()
		}
	}
	if s.localRequestAuthorized(r, token) {
		if agents, err := s.store.Agents(); err == nil {
			data["Agents"] = agents
		} else if pageError == "" {
			data["Error"] = err.Error()
		}
	}
	return data
}

func (s *Server) handleConnectionRequest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From    string `json:"from"`
		To      string `json:"to"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse("invalid_json"))
		return
	}
	id, err := s.sendTyped(req.From, []string{req.To}, protocol.MessageTypeConnectionRequest, map[string]any{"message": req.Message}, "", "")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message_id": id})
}

func (s *Server) handleConnectionAccept(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse("invalid_json"))
		return
	}
	payload := map[string]any{"permissions": protocol.DefaultConnectionPermissions()}
	id, err := s.sendTyped(req.From, []string{req.To}, protocol.MessageTypeConnectionAccept, payload, "", "")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message_id": id})
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	var req protocol.SendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse("invalid_json"))
		return
	}
	id, err := s.sendTyped(req.From, req.To, req.Type, req.Payload, "", "")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse(err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message_id": id})
}

func (s *Server) handleInbox(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent_id")
	if agentID == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse("missing_agent_id"))
		return
	}
	limit := 50
	unprocessed := r.URL.Query().Get("unprocessed") == "true"
	msgs, err := s.store.Inbox(agentID, limit, unprocessed)
	if err != nil {
		s.store.Log("error", "inbox_failed", err.Error(), map[string]string{"agent_id": agentID})
		writeJSON(w, http.StatusInternalServerError, errorResponse("inbox_failed: "+err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

func (s *Server) handleMessageAction(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/messages/")
	if !strings.HasSuffix(rest, "/ack") {
		http.NotFound(w, r)
		return
	}
	messageID := strings.TrimSuffix(rest, "/ack")
	if err := s.store.MarkProcessed(messageID); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse("ack_failed"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		From           string   `json:"from"`
		To             string   `json:"to"`
		Title          string   `json:"title"`
		Description    string   `json:"description"`
		Requirements   []string `json:"requirements"`
		MaxRevisions   int      `json:"max_revisions"`
		ExpectedFormat string   `json:"expected_format"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse("invalid_json"))
		return
	}
	taskID := protocol.NewID("task")
	conversationID := protocol.NewID("conv")
	payload := protocol.TaskRequestPayload{
		Title:          req.Title,
		Description:    req.Description,
		Requirements:   req.Requirements,
		MaxRevisions:   req.MaxRevisions,
		ExpectedFormat: req.ExpectedFormat,
	}
	msgID, err := s.sendTyped(req.From, []string{req.To}, protocol.MessageTypeTaskRequest, payload, conversationID, taskID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse(err.Error()))
		return
	}
	_ = s.store.UpsertTask(TaskRecord{
		TaskID:         taskID,
		ConversationID: conversationID,
		Creator:        req.From,
		Assignee:       req.To,
		Title:          req.Title,
		Description:    req.Description,
		Status:         protocol.TaskStatusSent,
		MaxRevisions:   req.MaxRevisions,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "task_id": taskID, "message_id": msgID})
}

func (s *Server) handleTaskAction(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/tasks/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	taskID, action := parts[0], parts[1]
	task, err := s.store.Task(taskID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorResponse("task_not_found"))
		return
	}
	var base struct {
		From             string   `json:"from"`
		Message          string   `json:"message"`
		Reason           string   `json:"reason"`
		RequestedChanges []string `json:"requested_changes"`
		ArtifactType     string   `json:"artifact_type"`
		Content          any      `json:"content"`
		Notes            string   `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&base); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse("invalid_json"))
		return
	}
	to := task.Creator
	if base.From == task.Creator {
		to = task.Assignee
	}
	var msgType protocol.MessageType
	var payload any
	var status protocol.TaskStatus
	switch action {
	case "accept":
		msgType = protocol.MessageTypeTaskAccept
		payload = protocol.TaskDecisionPayload{Message: base.Message}
		status = protocol.TaskStatusAccepted
	case "decline":
		msgType = protocol.MessageTypeTaskDecline
		payload = protocol.TaskDecisionPayload{Reason: base.Reason}
		status = protocol.TaskStatusDeclined
	case "artifacts":
		msgType = protocol.MessageTypeArtifactSubmit
		version := task.RevisionCount + 1
		payload = protocol.ArtifactSubmitPayload{Version: version, ArtifactType: base.ArtifactType, Content: base.Content, Notes: base.Notes}
		if version > 1 {
			status = protocol.TaskStatusResubmitted
		} else {
			status = protocol.TaskStatusSubmitted
		}
	case "revision":
		msgType = protocol.MessageTypeRevisionRequest
		task.RevisionCount++
		remaining := task.MaxRevisions - task.RevisionCount
		payload = protocol.RevisionRequestPayload{Reason: base.Reason, RequestedChanges: base.RequestedChanges, RemainingRevisions: remaining}
		status = protocol.TaskStatusRevisionRequested
	case "complete":
		msgType = protocol.MessageTypeTaskComplete
		payload = protocol.TaskDecisionPayload{Message: base.Message}
		status = protocol.TaskStatusCompleted
	default:
		http.NotFound(w, r)
		return
	}
	msgID, err := s.sendTyped(base.From, []string{to}, msgType, payload, task.ConversationID, task.TaskID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse(err.Error()))
		return
	}
	task.Status = status
	_ = s.store.UpsertTask(task)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message_id": msgID})
}

func (s *Server) sendTyped(from string, to []string, msgType protocol.MessageType, payload any, conversationID string, taskID string) (string, error) {
	cfg := s.config()
	if len(to) != 1 {
		return "", errors.New("only_one_recipient_supported_in_core")
	}
	fromAgent, err := s.store.Agent(from)
	if err != nil {
		return "", errors.New("from_agent_not_found")
	}
	recipient, err := s.resolveAgent(to[0])
	if err != nil {
		return "", fmt.Errorf("recipient_resolve_failed: %w", err)
	}
	encrypted, err := protocol.EncryptPayloadJSON(payload, recipient.EncryptionPublicKey)
	if err != nil {
		return "", err
	}
	env := protocol.NewEnvelope(msgType, from, to, encrypted)
	env.ConversationID = conversationID
	env.TaskID = taskID
	env.Metadata["client_id"] = cfg.ClientID
	env.Metadata["device_id"] = cfg.DeviceID
	env.SigningKeyID = fromAgent.Handle
	if err := protocol.SignEnvelope(&env, fromAgent.SigningPrivateKey); err != nil {
		return "", err
	}
	plain, _ := json.Marshal(payload)
	if err := s.store.SaveMessage(env, "outbound", plain, protocol.DeliveryPending, protocol.ProcessingProcessed); err != nil {
		return "", err
	}
	s.enqueueRelaySend(env)
	return env.ID, nil
}

func (s *Server) relayLoop() {
	for {
		if err := s.connectRelayOnce(); err != nil {
			s.setRelayConnected(false)
			s.store.Log("warn", "relay_disconnected", err.Error(), nil)
			time.Sleep(2 * time.Second)
		}
	}
}

func (s *Server) connectRelayOnce() error {
	cfg := s.config()
	u, err := url.Parse(cfg.RelayWS)
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("client_id", cfg.ClientID)
	if cfg.RelayToken != "" {
		q.Set("token", cfg.RelayToken)
	}
	u.RawQuery = q.Encode()
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	s.setRelayConnected(true)
	if err := s.registerAllAgents(); err != nil {
		s.store.Log("warn", "relay_register_all_failed", err.Error(), nil)
	}
	s.flushOutbox()
	done := make(chan error, 1)
	go s.relayReadLoop(conn, done)
	for {
		select {
		case frame := <-s.relaySend:
			if err := conn.WriteJSON(frame); err != nil {
				return err
			}
		case err := <-done:
			return err
		case <-s.relayReset:
			return errors.New("relay_config_changed")
		}
	}
}

func (s *Server) relayReadLoop(conn *websocket.Conn, done chan<- error) {
	for {
		var frame protocol.RelayFrame
		if err := conn.ReadJSON(&frame); err != nil {
			done <- err
			return
		}
		switch frame.Kind {
		case "relay_ack":
			if frame.MessageID != "" {
				_ = s.store.MarkDelivery(frame.MessageID, protocol.DeliveryRelayAccepted)
			}
		case "relay_error":
			s.store.Log("warn", "relay_send_failed", frame.Error, map[string]string{"message_id": frame.MessageID})
		case "relay_deliver":
			if frame.Envelope != nil {
				if err := s.handleInbound(*frame.Envelope); err != nil {
					s.store.Log("warn", "inbound_failed", err.Error(), map[string]string{"message_id": frame.Envelope.ID})
				} else {
					_ = conn.WriteJSON(protocol.RelayFrame{Kind: "client_ack", MessageID: frame.Envelope.ID})
				}
			}
		}
	}
}

func (s *Server) handleInbound(env protocol.Envelope) error {
	var local AgentRecord
	var found bool
	for _, recipient := range env.To {
		agent, err := s.store.Agent(recipient)
		if err == nil {
			local = agent
			found = true
			break
		}
	}
	if !found {
		return errors.New("no_local_recipient")
	}
	plain, err := protocol.DecryptPayload(env.Payload, local.EncryptionPrivateKey, local.EncryptionPublicKey)
	if err != nil {
		return err
	}
	if err := s.store.SaveMessage(env, "inbound", plain, protocol.DeliveryDeliveredToClient, protocol.ProcessingUnread); err != nil {
		return err
	}
	s.applyTaskProjection(env, plain)
	return nil
}

func (s *Server) applyTaskProjection(env protocol.Envelope, plain []byte) {
	switch env.Type {
	case protocol.MessageTypeTaskRequest:
		var payload protocol.TaskRequestPayload
		if json.Unmarshal(plain, &payload) == nil {
			_ = s.store.UpsertTask(TaskRecord{
				TaskID:         env.TaskID,
				ConversationID: env.ConversationID,
				Creator:        env.From,
				Assignee:       env.To[0],
				Title:          payload.Title,
				Description:    payload.Description,
				Status:         protocol.TaskStatusSent,
				MaxRevisions:   payload.MaxRevisions,
			})
		}
	case protocol.MessageTypeTaskAccept:
		s.updateTaskStatus(env.TaskID, protocol.TaskStatusAccepted, 0)
	case protocol.MessageTypeTaskDecline:
		s.updateTaskStatus(env.TaskID, protocol.TaskStatusDeclined, 0)
	case protocol.MessageTypeArtifactSubmit:
		var payload protocol.ArtifactSubmitPayload
		delta := 0
		status := protocol.TaskStatusSubmitted
		if json.Unmarshal(plain, &payload) == nil && payload.Version > 1 {
			status = protocol.TaskStatusResubmitted
		}
		s.updateTaskStatus(env.TaskID, status, delta)
	case protocol.MessageTypeRevisionRequest:
		s.updateTaskStatus(env.TaskID, protocol.TaskStatusRevisionRequested, 1)
	case protocol.MessageTypeTaskComplete:
		s.updateTaskStatus(env.TaskID, protocol.TaskStatusCompleted, 0)
	case protocol.MessageTypeTaskCancel:
		s.updateTaskStatus(env.TaskID, protocol.TaskStatusCancelled, 0)
	}
}

func (s *Server) updateTaskStatus(taskID string, status protocol.TaskStatus, revisionDelta int) {
	task, err := s.store.Task(taskID)
	if err != nil {
		return
	}
	task.Status = status
	task.RevisionCount += revisionDelta
	_ = s.store.UpsertTask(task)
}

func (s *Server) flushOutbox() {
	pending, err := s.store.PendingOutbound(100)
	if err != nil {
		return
	}
	for _, env := range pending {
		s.enqueueRelaySend(env)
	}
}

func (s *Server) enqueueRelaySend(env protocol.Envelope) {
	select {
	case s.relaySend <- protocol.RelayFrame{Kind: "relay_send", Envelope: &env}:
	default:
		s.store.Log("warn", "relay_send_queue_full", "outbound frame queued in local database only", map[string]string{"message_id": env.ID})
	}
}

func (s *Server) registerAllAgents() error {
	agents, err := s.store.Agents()
	if err != nil {
		return err
	}
	for _, agent := range agents {
		if err := s.registerAgent(agent); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) registerAgent(agent AgentRecord) error {
	cfg := s.config()
	req := protocol.RegisterAgentRequest{ClientID: cfg.ClientID, DeviceID: cfg.DeviceID, Agent: agent.Profile()}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(req); err != nil {
		return err
	}
	httpReq, err := http.NewRequest(http.MethodPost, strings.TrimRight(cfg.RelayHTTP, "/")+"/v1/agents/register", &buf)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-TaskFerry-Client-ID", cfg.ClientID)
	if cfg.RelayToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+cfg.RelayToken)
	}
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("relay register status %d", resp.StatusCode)
	}
	return nil
}

func (s *Server) resolveAgent(handle string) (protocol.AgentProfile, error) {
	cfg := s.config()
	u := strings.TrimRight(cfg.RelayHTTP, "/") + "/v1/agents/resolve?handle=" + url.QueryEscape(handle)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return protocol.AgentProfile{}, err
	}
	if cfg.RelayToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.RelayToken)
	}
	req.Header.Set("X-TaskFerry-Client-ID", cfg.ClientID)
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return protocol.AgentProfile{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return protocol.AgentProfile{}, fmt.Errorf("relay resolve status %d", resp.StatusCode)
	}
	var out protocol.ResolveAgentResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return protocol.AgentProfile{}, err
	}
	if !out.OK || out.Agent == nil {
		return protocol.AgentProfile{}, errors.New(out.Error)
	}
	return *out.Agent, nil
}

func (s *Server) fetchAgentInvite(handle string) (protocol.InviteResponse, error) {
	cfg := s.config()
	u := strings.TrimRight(cfg.RelayHTTP, "/") + "/v1/agents/invite?handle=" + url.QueryEscape(handle)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return protocol.InviteResponse{}, err
	}
	req.Header.Set("X-TaskFerry-Client-ID", cfg.ClientID)
	if cfg.RelayToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.RelayToken)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return protocol.InviteResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return protocol.InviteResponse{}, fmt.Errorf("relay invite status %d", resp.StatusCode)
	}
	var out protocol.InviteResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return protocol.InviteResponse{}, err
	}
	if !out.OK {
		return protocol.InviteResponse{}, errors.New(out.Error)
	}
	return out, nil
}

func (s *Server) resolveInvite(rawInvite string) (protocol.InviteResponse, error) {
	endpoint, err := s.inviteResolveEndpoint(rawInvite)
	if err != nil {
		return protocol.InviteResponse{}, err
	}
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return protocol.InviteResponse{}, err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return protocol.InviteResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return protocol.InviteResponse{}, fmt.Errorf("relay invite status %d", resp.StatusCode)
	}
	var out protocol.InviteResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return protocol.InviteResponse{}, err
	}
	if !out.OK {
		return protocol.InviteResponse{}, errors.New(out.Error)
	}
	return out, nil
}

func (s *Server) inviteResolveEndpoint(rawInvite string) (string, error) {
	cfg := s.config()
	rawInvite = strings.TrimSpace(rawInvite)
	if rawInvite == "" {
		return "", errors.New("missing_invite")
	}
	if !strings.Contains(rawInvite, "://") {
		return strings.TrimRight(cfg.RelayHTTP, "/") + "/v1/invites/" + url.PathEscape(rawInvite), nil
	}
	inviteURL, err := url.Parse(rawInvite)
	if err != nil {
		return "", err
	}
	var host string
	var code string
	switch inviteURL.Scheme {
	case "taskferry":
		host = inviteURL.Host
		code = strings.TrimPrefix(inviteURL.EscapedPath(), "/invite/")
	case "http", "https":
		host = inviteURL.Host
		if strings.HasPrefix(inviteURL.EscapedPath(), "/invite/") {
			code = strings.TrimPrefix(inviteURL.EscapedPath(), "/invite/")
		} else if strings.HasPrefix(inviteURL.EscapedPath(), "/v1/invites/") {
			code = strings.TrimPrefix(inviteURL.EscapedPath(), "/v1/invites/")
		}
	default:
		return "", errors.New("unsupported_invite_scheme")
	}
	if host == "" || code == "" {
		return "", errors.New("invalid_invite_url")
	}
	relayURL, err := url.Parse(cfg.RelayHTTP)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(host, relayURL.Host) {
		return "", fmt.Errorf("invite_relay_mismatch: configured %s but invite uses %s", relayURL.Host, host)
	}
	code, err = url.PathUnescape(code)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(cfg.RelayHTTP, "/") + "/v1/invites/" + url.PathEscape(code), nil
}

func (s *Server) setRelayConnected(value bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.relayConnected = value
}

func (s *Server) config() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Server) applyConfig(cfg Config) {
	s.mu.Lock()
	s.cfg = cfg
	s.relayConnected = false
	s.mu.Unlock()
	select {
	case s.relayReset <- struct{}{}:
	default:
	}
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	agents, _ := s.store.Agents()
	messages, _ := s.store.RecentMessages(20)
	tasks, _ := s.store.Tasks(20)
	s.mu.RLock()
	relayConnected := s.relayConnected
	cfg := s.cfg
	s.mu.RUnlock()
	data := map[string]any{
		"ClientID":       cfg.ClientID,
		"DeviceID":       cfg.DeviceID,
		"RelayConnected": relayConnected,
		"Agents":         agents,
		"Messages":       messages,
		"Tasks":          tasks,
	}
	if err := dashboardTemplate.Execute(w, data); err != nil {
		log.Printf("dashboard render failed: %v", err)
	}
}

func (s *Server) handleTasksPage(w http.ResponseWriter, r *http.Request) {
	if acceptsHTML(r) {
		s.handleDashboard(w, r)
		return
	}
	tasks, err := s.store.Tasks(100)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse("tasks_failed"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-TaskFerry-Local-Token")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withLocalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg := s.config()
		if cfg.LocalAPIToken == "" || r.URL.Path == "/health" || r.URL.Path == "/connect" || r.URL.Path == "/setup" {
			next.ServeHTTP(w, r)
			return
		}
		if !s.localRequestAuthorized(r, "") {
			writeJSON(w, http.StatusUnauthorized, errorResponse("unauthorized"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) localRequestAuthorized(r *http.Request, explicitToken string) bool {
	cfg := s.config()
	if cfg.LocalAPIToken == "" {
		return true
	}
	return tokenOK(explicitToken, cfg.LocalAPIToken) ||
		tokenOK(r.Header.Get("Authorization"), cfg.LocalAPIToken) ||
		tokenOK(r.Header.Get("X-TaskFerry-Local-Token"), cfg.LocalAPIToken) ||
		tokenOK(r.URL.Query().Get("token"), cfg.LocalAPIToken)
}

func localTokenFromRequest(r *http.Request) string {
	if value := r.URL.Query().Get("token"); value != "" {
		return value
	}
	if value := r.Header.Get("X-TaskFerry-Local-Token"); value != "" {
		return value
	}
	if value := r.Header.Get("Authorization"); value != "" {
		return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "Bearer "))
	}
	return ""
}

func tokenOK(value string, expected string) bool {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "bearer ") {
		value = strings.TrimSpace(value[len("bearer "):])
	}
	if value == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(value), []byte(expected)) == 1
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func errorResponse(code string) map[string]any {
	return map[string]any{"ok": false, "error": code}
}

func acceptsHTML(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

func parseCSV(value string) []string {
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func suggestedHandle(ownerName string, clientID string) string {
	base := strings.ToLower(strings.TrimSpace(ownerName))
	if base == "" {
		base = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(clientID)), "client_")
	}
	var b strings.Builder
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	slug := strings.Trim(b.String(), "-_")
	if slug == "" {
		slug = "you"
	}
	if len(slug) > 24 {
		slug = slug[:24]
	}
	return "@" + slug + "/agent"
}

var dashboardTemplate = template.Must(template.New("dashboard").Parse(`<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <title>TaskFerry Local</title>
  <style>
    body { font-family: Segoe UI, Arial, sans-serif; margin: 24px; color: #1b1f24; background: #f7f8fa; }
    main { max-width: 1180px; margin: 0 auto; }
    h1 { font-size: 24px; margin: 0 0 16px; }
    h2 { font-size: 18px; margin: 28px 0 10px; }
    .status { display: flex; gap: 12px; flex-wrap: wrap; }
    .pill { background: #fff; border: 1px solid #d9dee7; border-radius: 6px; padding: 8px 10px; }
    table { width: 100%; border-collapse: collapse; background: #fff; border: 1px solid #d9dee7; }
    th, td { padding: 8px 10px; border-bottom: 1px solid #edf0f4; text-align: left; vertical-align: top; font-size: 13px; }
    th { background: #eef2f6; font-weight: 600; }
    code, pre { font-family: Consolas, monospace; font-size: 12px; }
    pre { margin: 0; white-space: pre-wrap; max-height: 180px; overflow: auto; }
  </style>
</head>
<body>
<main>
  <h1>TaskFerry Local Client</h1>
  <div class="status">
    <div class="pill">client: <code>{{.ClientID}}</code></div>
    <div class="pill">device: <code>{{.DeviceID}}</code></div>
    <div class="pill">relay: <code>{{.RelayConnected}}</code></div>
  </div>
  <h2>Agents</h2>
  <table><tr><th>Handle</th><th>Name</th><th>Capabilities</th></tr>
  {{range .Agents}}<tr><td><code>{{.Handle}}</code></td><td>{{.DisplayName}}</td><td>{{range .Capabilities}}<code>{{.}}</code> {{end}}</td></tr>{{end}}
  </table>
  <h2>Tasks</h2>
  <table><tr><th>Task</th><th>Title</th><th>Creator</th><th>Assignee</th><th>Status</th><th>Revisions</th></tr>
  {{range .Tasks}}<tr><td><code>{{.TaskID}}</code></td><td>{{.Title}}</td><td><code>{{.Creator}}</code></td><td><code>{{.Assignee}}</code></td><td>{{.Status}}</td><td>{{.RevisionCount}} / {{.MaxRevisions}}</td></tr>{{end}}
  </table>
  <h2>Recent Messages</h2>
  <table><tr><th>Time</th><th>Direction</th><th>Type</th><th>From</th><th>To</th><th>Task</th><th>Payload</th></tr>
  {{range .Messages}}<tr><td>{{.CreatedAt}}</td><td>{{.Direction}}</td><td>{{.Type}}</td><td><code>{{.Sender}}</code></td><td>{{range .Recipients}}<code>{{.}}</code> {{end}}</td><td><code>{{.TaskID}}</code></td><td><pre>{{printf "%s" .Plaintext}}</pre></td></tr>{{end}}
  </table>
</main>
</body>
</html>`))

var setupTemplate = template.Must(template.New("setup").Parse(`<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <title>TaskFerry Setup</title>
  <style>
    :root { --ink:#171915; --muted:#5f675a; --paper:#fbfaf4; --panel:#fff; --line:#d9decf; --green:#b9f04a; --red:#a6362f; }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background:
        linear-gradient(90deg, rgba(23,25,21,.045) 1px, transparent 1px) 0 0 / 32px 32px,
        linear-gradient(rgba(23,25,21,.035) 1px, transparent 1px) 0 0 / 32px 32px,
        var(--paper);
      color: var(--ink);
      font-family: "Segoe UI", system-ui, sans-serif;
    }
    main { max-width: 900px; margin: 0 auto; padding: 36px 20px 70px; }
    h1 { font-family: Georgia, "Times New Roman", serif; font-size: 56px; line-height: .95; margin: 0 0 18px; }
    h2 { font-size: 20px; margin: 0 0 12px; }
    .lead, .muted { color: var(--muted); line-height: 1.5; }
    .panel { background: rgba(255,255,255,.88); border: 1px solid var(--line); border-radius: 8px; padding: 18px; margin: 18px 0; }
    .success { border-color: #89b84f; background: #f3ffe2; }
    .error { color: var(--red); font-weight: 700; }
    .warning { color: #8a5a00; font-weight: 700; }
    label { display:block; font-weight:700; margin:14px 0 6px; }
    input, textarea {
      width:100%;
      border:1px solid #cdd4c6;
      border-radius:7px;
      min-height:42px;
      padding:9px 10px;
      font:inherit;
      background:#fff;
    }
    textarea { min-height:76px; resize:vertical; }
    .check { display:flex; align-items:center; gap:8px; margin-top:14px; font-weight:700; }
    .check input { width:auto; min-height:0; }
    code { font-family: Consolas, "Cascadia Mono", monospace; font-size:13px; overflow-wrap:anywhere; }
    button, .button {
      display:inline-flex;
      min-height:44px;
      align-items:center;
      justify-content:center;
      border:2px solid var(--ink);
      background:var(--ink);
      color:white;
      text-decoration:none;
      padding:10px 14px;
      border-radius:7px;
      font-weight:800;
      box-shadow:5px 5px 0 var(--green);
      cursor:pointer;
      margin-top:16px;
    }
    .grid { display:grid; grid-template-columns:1fr 1fr; gap:12px; }
    @media (max-width:720px) { h1 { font-size:42px; } .grid { grid-template-columns:1fr; } }
  </style>
</head>
<body>
<main>
  <h1>Set up this local agent.</h1>
  <p class="lead">This page saves your relay credential into the local TaskFerry client, then creates the agent profile that can appear in the public directory.</p>

  {{if .Saved}}
  <section class="panel success">
    <h2>Local setup saved</h2>
    <p class="muted">Client <code>{{.ClientID}}</code> is configured for <code>{{.RelayHTTP}}</code>.</p>
    {{if .HasAgent}}
    <p class="muted">Agent <code>{{.Agent.Handle}}</code> was created locally.</p>
    {{if .InviteURL}}<p class="muted">Invite: <code>{{.InviteURL}}</code></p>{{end}}
    {{if .WebInviteURL}}<p><a class="button" href="{{.WebInviteURL}}" rel="noreferrer" referrerpolicy="no-referrer">Open public invite</a></p>{{end}}
    {{end}}
    <p><a class="button" href="/?token={{.Token}}">Open local dashboard</a></p>
  </section>
  {{end}}

  {{if .Error}}<section class="panel"><p class="error">{{.Error}}</p></section>{{end}}
  {{range .Warnings}}<section class="panel"><p class="warning">{{.}}</p></section>{{end}}

  <section class="panel">
    <h2>Relay credential</h2>
    <form method="post" action="/setup">
      <div class="grid">
        <div>
          <label for="client_id">client_id</label>
          <input id="client_id" name="client_id" value="{{.ClientID}}" required>
        </div>
        <div>
          <label for="relay_token">relay_token</label>
          <input id="relay_token" name="relay_token" value="{{.RelayToken}}" required>
        </div>
      </div>
      <div class="grid">
        <div>
          <label for="relay_http">Relay HTTP</label>
          <input id="relay_http" name="relay_http" value="{{.RelayHTTP}}" required>
        </div>
        <div>
          <label for="relay_ws">Relay WebSocket</label>
          <input id="relay_ws" name="relay_ws" value="{{.RelayWS}}" required>
        </div>
      </div>

      {{if .NeedsToken}}
      <label for="token">Local API token</label>
      <input id="token" name="token" value="{{.Token}}" type="password" autocomplete="off" placeholder="TASKFERRY_LOCAL_API_TOKEN">
      {{else}}
      <input type="hidden" name="token" value="{{.Token}}">
      {{end}}

      <h2 style="margin-top:24px">Agent profile</h2>
      <label for="handle">Handle</label>
      <input id="handle" name="handle" value="{{.Handle}}" placeholder="@you/agent">
      <label for="display_name">Display name</label>
      <input id="display_name" name="display_name" value="{{.DisplayName}}" placeholder="Your Agent">
      <label for="tagline">One-line intro</label>
      <input id="tagline" name="tagline" value="{{.Tagline}}" placeholder="Available for TaskFerry work.">
      <label for="capabilities">Capabilities</label>
      <input id="capabilities" name="capabilities" value="{{.Capabilities}}" placeholder="code,review">
      <label class="check"><input type="checkbox" name="public_profile" {{if .PublicProfile}}checked{{end}}> List this agent publicly</label>
      <button type="submit">Save local setup</button>
    </form>
  </section>
</main>
</body>
</html>`))

var connectTemplate = template.Must(template.New("connect").Parse(`<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <title>TaskFerry Connect</title>
  <style>
    :root { --ink:#171915; --muted:#5f675a; --paper:#fbfaf4; --panel:#fff; --line:#d9decf; --green:#b9f04a; --red:#a6362f; }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background:
        linear-gradient(90deg, rgba(23,25,21,.045) 1px, transparent 1px) 0 0 / 32px 32px,
        linear-gradient(rgba(23,25,21,.035) 1px, transparent 1px) 0 0 / 32px 32px,
        var(--paper);
      color: var(--ink);
      font-family: "Segoe UI", system-ui, sans-serif;
    }
    main { max-width: 860px; margin: 0 auto; padding: 36px 20px 70px; }
    h1 { font-family: Georgia, "Times New Roman", serif; font-size: 56px; line-height: .95; margin: 0 0 18px; }
    h2 { font-size: 20px; margin: 26px 0 12px; }
    .panel { background: rgba(255,255,255,.88); border: 1px solid var(--line); border-radius: 8px; padding: 18px; margin: 18px 0; }
    .profile { display: grid; gap: 8px; }
    .handle, code { font-family: Consolas, "Cascadia Mono", monospace; font-size: 13px; overflow-wrap: anywhere; }
    .name { font-size: 24px; font-weight: 800; }
    .muted { color: var(--muted); line-height: 1.5; }
    .error { color: var(--red); font-weight: 700; }
    .success { border-color: #89b84f; background: #f3ffe2; }
    label { display: block; font-weight: 700; margin: 14px 0 6px; }
    input, select, textarea {
      width: 100%;
      border: 1px solid #cdd4c6;
      border-radius: 7px;
      min-height: 42px;
      padding: 9px 10px;
      font: inherit;
      background: #fff;
    }
    textarea { min-height: 84px; resize: vertical; }
    button, .button {
      display: inline-flex;
      align-items: center;
      justify-content: center;
      min-height: 44px;
      border: 2px solid var(--ink);
      background: var(--ink);
      color: #fff;
      text-decoration: none;
      padding: 10px 14px;
      border-radius: 7px;
      font-weight: 800;
      box-shadow: 5px 5px 0 var(--green);
      cursor: pointer;
      margin-top: 16px;
    }
    .row { display: grid; grid-template-columns: 1fr; gap: 10px; }
    @media (max-width: 640px) { h1 { font-size: 42px; } }
  </style>
</head>
<body>
<main>
  <h1>Connect with this agent.</h1>

  {{if .Sent}}
  <section class="panel success">
    <h2>Request sent</h2>
    <p class="muted"><code>{{.From}}</code> requested a TaskFerry connection with <code>{{.To}}</code>.</p>
    <p class="muted">Message id: <code>{{.MessageID}}</code></p>
    <p><a class="button" href="/?token={{.Token}}">Back to local dashboard</a></p>
  </section>
  {{end}}

  {{if .Error}}
  <section class="panel"><p class="error">{{.Error}}</p></section>
  {{end}}

  {{if .InviteAgent}}
  <section class="panel profile">
    <div class="handle">{{.InviteAgent.Handle}}</div>
    <div class="name">{{if .InviteAgent.DisplayName}}{{.InviteAgent.DisplayName}}{{else}}{{.InviteAgent.Handle}}{{end}}</div>
    <div class="muted">{{if .InviteAgent.Tagline}}{{.InviteAgent.Tagline}}{{else}}{{.InviteAgent.Description}}{{end}}</div>
  </section>
  {{else}}
  <section class="panel">
    <p class="muted">Paste a TaskFerry invite URL to preview the agent and choose a local identity.</p>
  </section>
  {{end}}

  <section class="panel">
    <form method="post" action="/connect">
      <label for="invite">Invite</label>
      <input id="invite" name="invite" value="{{.InviteRaw}}" placeholder="taskferry://relay.meiyaku.com/invite/inv_...">

      {{if .NeedsToken}}
      <label for="token">Local API token</label>
      <input id="token" name="token" value="{{.Token}}" type="password" autocomplete="off" placeholder="TASKFERRY_LOCAL_API_TOKEN">
      <p class="muted">The invite preview is public. Choosing a local identity requires your local token.</p>
      {{else}}
      <input type="hidden" name="token" value="{{.Token}}">
      {{end}}

      {{if .Agents}}
      <label for="from">Use local identity</label>
      <select id="from" name="from">
        {{range .Agents}}
        <option value="{{.Handle}}">{{.Handle}}{{if .DisplayName}} - {{.DisplayName}}{{end}}</option>
        {{end}}
      </select>
      <label for="message">Request message</label>
      <textarea id="message" name="message">{{.Message}}</textarea>
      <button type="submit">Send connection request</button>
      {{else}}
      {{if not .NeedsToken}}
      <p class="muted">No local agent identities exist yet. Create one first from the local dashboard or CLI.</p>
      {{end}}
      <button type="submit">Continue</button>
      {{end}}
    </form>
  </section>
</main>
</body>
</html>`))
