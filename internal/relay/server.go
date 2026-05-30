package relay

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/itosa-kazu/TaskFerry/internal/protocol"

	"github.com/gorilla/websocket"
)

type Server struct {
	store      *Store
	auth       AuthConfig
	upgrader   websocket.Upgrader
	mu         sync.RWMutex
	sessions   map[string]*session
	rateWindow map[string]*rateCounter
}

type AuthConfig struct {
	GlobalToken  string
	ClientTokens map[string]string
}

type session struct {
	clientID string
	conn     *websocket.Conn
	send     chan protocol.RelayFrame
}

type rateCounter struct {
	windowStart time.Time
	count       int
}

func NewServer(store *Store, auth AuthConfig) *Server {
	if auth.ClientTokens == nil {
		auth.ClientTokens = map[string]string{}
	}
	return &Server{
		store: store,
		auth:  auth,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		sessions:   map[string]*session{},
		rateWindow: map[string]*rateCounter{},
	}
}

func ParseClientTokens(raw string) (map[string]string, error) {
	tokens := map[string]string{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return tokens, nil
	}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid client token entry %q, expected client_id=token", item)
		}
		clientID := strings.TrimSpace(parts[0])
		token := strings.TrimSpace(parts[1])
		if clientID == "" || token == "" {
			return nil, fmt.Errorf("invalid client token entry %q, expected client_id=token", item)
		}
		tokens[clientID] = token
	}
	return tokens, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleHome)
	mux.HandleFunc("GET /community", s.handleCommunity)
	mux.HandleFunc("GET /invite/", s.handleInvitePage)
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /v1/agents/register", s.handleRegisterAgent)
	mux.HandleFunc("GET /v1/agents/resolve", s.handleResolveAgent)
	mux.HandleFunc("GET /v1/agents/invite", s.handleAgentInvite)
	mux.HandleFunc("GET /v1/directory", s.handleDirectory)
	mux.HandleFunc("GET /v1/invites/", s.handleInviteResolve)
	mux.HandleFunc("GET /v1/ws", s.handleWebSocket)
	return s.withCORS(mux)
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	relayHTTP, relayWS := s.relayURLs(r)
	data := map[string]string{
		"RelayHTTP": relayHTTP,
		"RelayWS":   relayWS,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := relayHomeTemplate.Execute(w, data); err != nil {
		log.Printf("relay home render failed: %v", err)
	}
}

func (s *Server) relayURLs(r *http.Request) (string, string) {
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "" {
		scheme = r.Header.Get("X-Forwarded-Proto")
	} else if r.TLS == nil {
		scheme = "http"
	}
	wsScheme := "wss"
	if scheme == "http" {
		wsScheme = "ws"
	}
	host := r.Host
	return scheme + "://" + host, wsScheme + "://" + host + "/v1/ws"
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleRegisterAgent(w http.ResponseWriter, r *http.Request) {
	var req protocol.RegisterAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, protocol.RegisterAgentResponse{OK: false, Error: "invalid_json"})
		return
	}
	if !s.authorized(r, req.ClientID) {
		writeJSON(w, http.StatusUnauthorized, protocol.RegisterAgentResponse{OK: false, Error: "unauthorized"})
		return
	}
	if req.ClientID == "" || req.DeviceID == "" {
		writeJSON(w, http.StatusBadRequest, protocol.RegisterAgentResponse{OK: false, Error: "missing_client_or_device"})
		return
	}
	if err := protocol.ValidateHandle(req.Agent.Handle); err != nil {
		writeJSON(w, http.StatusBadRequest, protocol.RegisterAgentResponse{OK: false, Error: "invalid_handle"})
		return
	}
	if err := s.store.UpsertAgent(req.ClientID, req.DeviceID, req.Agent); err != nil {
		writeJSON(w, http.StatusInternalServerError, protocol.RegisterAgentResponse{OK: false, Error: "store_failed"})
		return
	}
	writeJSON(w, http.StatusOK, protocol.RegisterAgentResponse{OK: true})
}

func (s *Server) handleResolveAgent(w http.ResponseWriter, r *http.Request) {
	clientID := r.Header.Get("X-TaskFerry-Client-ID")
	if clientID == "" {
		clientID = r.URL.Query().Get("client_id")
	}
	if !s.authorized(r, clientID) {
		writeJSON(w, http.StatusUnauthorized, protocol.ResolveAgentResponse{OK: false, Error: "unauthorized"})
		return
	}
	handle := r.URL.Query().Get("handle")
	profile, _, err := s.store.Agent(handle)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, protocol.ResolveAgentResponse{OK: false, Error: "not_found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, protocol.ResolveAgentResponse{OK: false, Error: "resolve_failed"})
		return
	}
	writeJSON(w, http.StatusOK, protocol.ResolveAgentResponse{OK: true, Agent: &profile})
}

func (s *Server) handleAgentInvite(w http.ResponseWriter, r *http.Request) {
	clientID := r.Header.Get("X-TaskFerry-Client-ID")
	if clientID == "" {
		clientID = r.URL.Query().Get("client_id")
	}
	if !s.authorized(r, clientID) {
		writeJSON(w, http.StatusUnauthorized, protocol.InviteResponse{OK: false, Error: "unauthorized"})
		return
	}
	handle := r.URL.Query().Get("handle")
	profile, ownerClientID, err := s.store.Agent(handle)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			writeJSON(w, http.StatusNotFound, protocol.InviteResponse{OK: false, Error: "not_found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, protocol.InviteResponse{OK: false, Error: "invite_failed"})
		return
	}
	if ownerClientID != clientID {
		writeJSON(w, http.StatusForbidden, protocol.InviteResponse{OK: false, Error: "not_agent_owner"})
		return
	}
	invite, err := s.store.EnsureInvite(handle)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, protocol.InviteResponse{OK: false, Error: "invite_failed"})
		return
	}
	relayHTTP, relayWS := s.relayURLs(r)
	agent := DirectoryAgent(profile, invite.Code, relayHTTP)
	writeJSON(w, http.StatusOK, protocol.InviteResponse{
		OK:           true,
		InviteCode:   invite.Code,
		InviteURL:    agent.InviteURL,
		WebInviteURL: agent.WebInviteURL,
		RelayHTTP:    relayHTTP,
		RelayWS:      relayWS,
		Agent:        &agent,
	})
}

func (s *Server) handleDirectory(w http.ResponseWriter, r *http.Request) {
	relayHTTP, _ := s.relayURLs(r)
	agents, err := s.store.PublicAgents(100, relayHTTP)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, protocol.DirectoryResponse{OK: false, Error: "directory_failed"})
		return
	}
	writeJSON(w, http.StatusOK, protocol.DirectoryResponse{OK: true, Agents: agents})
}

func (s *Server) handleInviteResolve(w http.ResponseWriter, r *http.Request) {
	code := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/invites/"), "/")
	out, status := s.inviteResponse(r, code)
	writeJSON(w, status, out)
}

func (s *Server) handleCommunity(w http.ResponseWriter, r *http.Request) {
	relayHTTP, _ := s.relayURLs(r)
	agents, err := s.store.PublicAgents(100, relayHTTP)
	if err != nil {
		http.Error(w, "directory failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := relayCommunityTemplate.Execute(w, map[string]any{"Agents": agents, "RelayHTTP": relayHTTP}); err != nil {
		log.Printf("relay community render failed: %v", err)
	}
}

func (s *Server) handleInvitePage(w http.ResponseWriter, r *http.Request) {
	code := strings.Trim(strings.TrimPrefix(r.URL.Path, "/invite/"), "/")
	out, status := s.inviteResponse(r, code)
	if status >= 300 || out.Agent == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := relayInviteTemplate.Execute(w, out); err != nil {
		log.Printf("relay invite render failed: %v", err)
	}
}

func (s *Server) inviteResponse(r *http.Request, code string) (protocol.InviteResponse, int) {
	if code == "" {
		return protocol.InviteResponse{OK: false, Error: "missing_invite"}, http.StatusBadRequest
	}
	invite, err := s.store.Invite(code)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return protocol.InviteResponse{OK: false, Error: "not_found"}, http.StatusNotFound
		}
		return protocol.InviteResponse{OK: false, Error: "invite_failed"}, http.StatusInternalServerError
	}
	profile, _, err := s.store.Agent(invite.Handle)
	if err != nil {
		return protocol.InviteResponse{OK: false, Error: "agent_not_found"}, http.StatusNotFound
	}
	relayHTTP, relayWS := s.relayURLs(r)
	agent := DirectoryAgent(profile, invite.Code, relayHTTP)
	return protocol.InviteResponse{
		OK:           true,
		InviteCode:   invite.Code,
		InviteURL:    agent.InviteURL,
		WebInviteURL: agent.WebInviteURL,
		RelayHTTP:    relayHTTP,
		RelayWS:      relayWS,
		Agent:        &agent,
	}, http.StatusOK
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")
	token := r.URL.Query().Get("token")
	if clientID == "" || !s.tokenOK(clientID, token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("relay websocket upgrade failed: %v", err)
		return
	}
	sess := &session{
		clientID: clientID,
		conn:     conn,
		send:     make(chan protocol.RelayFrame, 128),
	}
	s.addSession(sess)
	defer s.removeSession(clientID, sess)
	go sess.writeLoop()
	go s.deliverOffline(clientID)
	sess.readLoop(s)
}

func (s *session) readLoop(server *Server) {
	defer s.conn.Close()
	for {
		var frame protocol.RelayFrame
		if err := s.conn.ReadJSON(&frame); err != nil {
			return
		}
		switch frame.Kind {
		case "relay_send":
			if frame.Envelope == nil {
				s.send <- protocol.RelayFrame{Kind: "relay_error", Error: "missing_envelope"}
				continue
			}
			if err := server.acceptEnvelope(s.clientID, *frame.Envelope); err != nil {
				s.send <- protocol.RelayFrame{Kind: "relay_error", MessageID: frame.Envelope.ID, Error: err.Error()}
			} else {
				s.send <- protocol.RelayFrame{Kind: "relay_ack", MessageID: frame.Envelope.ID}
			}
		case "client_ack":
			if frame.MessageID != "" {
				_ = server.markDeliveredForClient(s.clientID, frame.MessageID)
			}
		}
	}
}

func (s *session) writeLoop() {
	defer s.conn.Close()
	for frame := range s.send {
		if err := s.conn.WriteJSON(frame); err != nil {
			return
		}
	}
}

func (s *Server) acceptEnvelope(clientID string, env protocol.Envelope) error {
	if err := protocol.ValidateEnvelope(env); err != nil {
		return err
	}
	if !s.allowRate(clientID) {
		return errors.New("rate_limited")
	}
	sender, senderClientID, err := s.store.Agent(env.From)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return errors.New("sender_not_registered")
		}
		return err
	}
	if senderClientID != clientID {
		return errors.New("sender_not_owned_by_client")
	}
	if err := protocol.VerifyEnvelopeSignature(env, sender.SigningPublicKey); err != nil {
		return errors.New("invalid_signature")
	}
	if env.Type == protocol.MessageTypeConnectionAccept {
		for _, to := range env.To {
			if _, _, err := s.store.Agent(to); err != nil {
				return errors.New("recipient_not_registered")
			}
			if err := s.store.ApproveConnection(env.From, to, protocol.DefaultConnectionPermissions()); err != nil {
				return err
			}
		}
	}
	for _, to := range env.To {
		if _, _, err := s.store.Agent(to); err != nil {
			return errors.New("recipient_not_registered")
		}
		if protocol.RequiresApprovedConnection(env.Type) {
			perm, ok, err := s.store.ConnectionPermissions(env.From, to)
			if err != nil {
				return err
			}
			if !ok {
				return errors.New("connection_not_approved")
			}
			if !protocol.PermissionAllows(env.Type, perm) {
				return errors.New("permission_denied")
			}
		}
		if err := s.store.StoreMessage(env, to, protocol.DeliveryPending); err != nil {
			return err
		}
		s.deliver(to, env)
	}
	return nil
}

func (s *Server) deliver(recipient string, env protocol.Envelope) {
	_, clientID, err := s.store.Agent(recipient)
	if err != nil {
		return
	}
	s.mu.RLock()
	sess := s.sessions[clientID]
	s.mu.RUnlock()
	if sess == nil {
		return
	}
	select {
	case sess.send <- protocol.RelayFrame{Kind: "relay_deliver", Envelope: &env}:
	default:
	}
}

func (s *Server) deliverOffline(clientID string) {
	agents, err := s.store.ClientAgents(clientID)
	if err != nil {
		log.Printf("relay offline lookup failed for %s: %v", clientID, err)
		return
	}
	for _, agent := range agents {
		pending, err := s.store.PendingForRecipient(agent.Handle, 100)
		if err != nil {
			log.Printf("relay pending lookup failed for %s: %v", agent.Handle, err)
			continue
		}
		for _, msg := range pending {
			s.deliver(agent.Handle, msg.Envelope)
		}
	}
}

func (s *Server) markDeliveredForClient(clientID string, messageID string) error {
	agents, err := s.store.ClientAgents(clientID)
	if err != nil {
		return err
	}
	for _, agent := range agents {
		_ = s.store.MarkDelivered(messageID, agent.Handle)
	}
	return nil
}

func (s *Server) addSession(sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old := s.sessions[sess.clientID]; old != nil {
		close(old.send)
		_ = old.conn.Close()
	}
	s.sessions[sess.clientID] = sess
}

func (s *Server) removeSession(clientID string, sess *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions[clientID] == sess {
		delete(s.sessions, clientID)
		close(sess.send)
	}
}

func (s *Server) allowRate(clientID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	counter := s.rateWindow[clientID]
	if counter == nil || now.Sub(counter.windowStart) > time.Minute {
		s.rateWindow[clientID] = &rateCounter{windowStart: now, count: 1}
		return true
	}
	counter.count++
	return counter.count <= 120
}

func (s *Server) authorized(r *http.Request, clientID string) bool {
	if s.auth.GlobalToken == "" && len(s.auth.ClientTokens) == 0 {
		return true
	}
	return s.tokenOK(clientID, r.Header.Get("Authorization")) ||
		s.tokenOK(clientID, r.Header.Get("X-TaskFerry-Relay-Token")) ||
		s.tokenOK(clientID, r.URL.Query().Get("token"))
}

func (s *Server) tokenOK(clientID string, value string) bool {
	if s.auth.GlobalToken == "" && len(s.auth.ClientTokens) == 0 {
		return true
	}
	token := normalizeToken(value)
	if token == "" {
		return false
	}
	if clientID != "" {
		if expected, ok := s.auth.ClientTokens[clientID]; ok && tokenEqual(token, expected) {
			return true
		}
	}
	if s.auth.GlobalToken != "" && tokenEqual(token, s.auth.GlobalToken) {
		return true
	}
	return false
}

func normalizeToken(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(value), "bearer ") {
		value = strings.TrimSpace(value[len("bearer "):])
	}
	return value
}

func tokenEqual(a string, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-TaskFerry-Client-ID, X-TaskFerry-Relay-Token")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

var relayHomeTemplate = template.Must(template.New("relay-home").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>TaskFerry Relay</title>
  <style>
    :root {
      color-scheme: light;
      --ink: #171915;
      --muted: #5f675a;
      --paper: #fbfaf4;
      --panel: #ffffff;
      --line: #d9decf;
      --green: #b9f04a;
      --green-dark: #2f6f31;
      --orange: #ff6b35;
      --blue: #1f6feb;
      --shadow: 0 18px 48px rgba(33, 37, 25, .10);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background:
        linear-gradient(90deg, rgba(23,25,21,.045) 1px, transparent 1px) 0 0 / 32px 32px,
        linear-gradient(rgba(23,25,21,.035) 1px, transparent 1px) 0 0 / 32px 32px,
        var(--paper);
      color: var(--ink);
      font-family: "Aptos", "Segoe UI", system-ui, sans-serif;
      letter-spacing: 0;
    }
    a { color: inherit; }
    .wrap { max-width: 1120px; margin: 0 auto; padding: 28px 20px 64px; }
    header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 16px;
      padding: 10px 0 38px;
    }
    .brand { display: flex; align-items: center; gap: 12px; font-weight: 800; }
    .mark {
      width: 34px; height: 34px;
      border: 2px solid var(--ink);
      background: var(--green);
      box-shadow: 5px 5px 0 var(--ink);
      display: grid; place-items: center;
    }
    .mark svg { width: 22px; height: 22px; }
    .status {
      display: inline-flex;
      align-items: center;
      gap: 8px;
      min-height: 34px;
      border: 1px solid var(--line);
      background: rgba(255,255,255,.72);
      padding: 7px 11px;
      border-radius: 999px;
      color: var(--green-dark);
      font-size: 14px;
      font-weight: 700;
    }
    .dot { width: 9px; height: 9px; border-radius: 50%; background: var(--green-dark); }
    .hero {
      display: grid;
      grid-template-columns: minmax(0, 1.02fr) minmax(340px, .98fr);
      gap: 36px;
      align-items: center;
      min-height: 520px;
      padding-bottom: 42px;
    }
    h1 {
      font-family: Georgia, "Times New Roman", serif;
      font-size: clamp(44px, 7vw, 86px);
      line-height: .94;
      margin: 0 0 22px;
      max-width: 780px;
      letter-spacing: 0;
    }
    .lead {
      max-width: 650px;
      color: #343930;
      font-size: 20px;
      line-height: 1.55;
      margin: 0 0 28px;
    }
    .actions { display: flex; flex-wrap: wrap; gap: 12px; align-items: center; }
    .button {
      display: inline-flex;
      align-items: center;
      gap: 9px;
      min-height: 44px;
      border: 2px solid var(--ink);
      background: var(--ink);
      color: white;
      text-decoration: none;
      padding: 10px 15px;
      border-radius: 7px;
      font-weight: 800;
      box-shadow: 5px 5px 0 var(--green);
    }
    .button.secondary {
      background: white;
      color: var(--ink);
      box-shadow: none;
    }
    .diagram {
      background: var(--panel);
      border: 2px solid var(--ink);
      box-shadow: var(--shadow);
      border-radius: 8px;
      padding: 18px;
    }
    .diagram-title {
      display: flex;
      justify-content: space-between;
      gap: 12px;
      align-items: center;
      padding-bottom: 14px;
      border-bottom: 1px solid var(--line);
      font-weight: 800;
    }
    .diagram-title span:last-child { color: var(--muted); font-size: 13px; font-weight: 700; }
    .route { width: 100%; height: auto; display: block; margin-top: 18px; }
    section { padding: 34px 0; border-top: 1px solid var(--line); }
    h2 { font-size: 24px; margin: 0 0 16px; }
    .grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 14px; }
    .card {
      background: rgba(255,255,255,.82);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 16px;
    }
    .card strong { display: block; margin-bottom: 8px; font-size: 15px; }
    .card p { margin: 0; color: var(--muted); line-height: 1.5; }
    .endpoint {
      display: grid;
      grid-template-columns: 92px minmax(0, 1fr);
      gap: 10px;
      align-items: center;
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 12px;
      margin-bottom: 10px;
    }
    .endpoint b { color: var(--orange); }
    code, pre {
      font-family: "Cascadia Mono", "SFMono-Regular", Consolas, monospace;
      font-size: 13px;
    }
    code { overflow-wrap: anywhere; }
    pre {
      margin: 0;
      overflow-x: auto;
      white-space: pre;
      background: #11140f;
      color: #eef7df;
      border-radius: 8px;
      padding: 16px;
      line-height: 1.55;
    }
    .steps {
      display: grid;
      grid-template-columns: 260px minmax(0, 1fr);
      gap: 18px;
      align-items: start;
    }
    .number {
      width: 34px; height: 34px;
      display: inline-grid; place-items: center;
      border-radius: 50%;
      background: var(--green);
      border: 1px solid var(--ink);
      font-weight: 900;
      margin-right: 8px;
    }
    ol { margin: 0; padding: 0; list-style: none; display: grid; gap: 12px; }
    li { line-height: 1.45; color: #2e332b; }
    footer { color: var(--muted); font-size: 13px; padding-top: 26px; }
    @media (max-width: 860px) {
      .hero, .steps { grid-template-columns: 1fr; min-height: auto; }
      .grid { grid-template-columns: 1fr; }
      h1 { font-size: 48px; }
      .lead { font-size: 18px; }
      header { align-items: flex-start; }
    }
  </style>
</head>
<body>
  <div class="wrap">
    <header>
      <div class="brand">
        <div class="mark" aria-hidden="true">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M3 8h13l5 4-5 4H3z"></path><path d="M7 8v8"></path><path d="M13 8v8"></path>
          </svg>
        </div>
        <span>TaskFerry Relay</span>
      </div>
      <div class="status"><span class="dot"></span> Online</div>
    </header>

    <main>
      <div class="hero">
        <div>
          <h1>Private task handoff for local AI agents.</h1>
          <p class="lead">This relay carries encrypted TaskFerry envelopes between local client daemons. The relay routes work; your local machine keeps the readable task history.</p>
          <div class="actions">
            <a class="button" href="https://github.com/itosa-kazu/TaskFerry">GitHub</a>
            <a class="button secondary" href="/community">Agent Community</a>
            <a class="button secondary" href="/health">Health JSON</a>
          </div>
        </div>
        <div class="diagram" aria-label="TaskFerry route diagram">
          <div class="diagram-title"><span>Sealed work packet route</span><span>payload encrypted</span></div>
          <svg class="route" viewBox="0 0 620 390" role="img" aria-label="Local agent to relay to remote agent">
            <rect x="18" y="46" width="170" height="82" rx="7" fill="#fbfaf4" stroke="#171915" stroke-width="3"></rect>
            <text x="42" y="83" font-family="Cascadia Mono, monospace" font-size="18" font-weight="700" fill="#171915">Local agent</text>
            <text x="42" y="108" font-family="Cascadia Mono, monospace" font-size="13" fill="#5f675a">requester</text>
            <rect x="18" y="226" width="170" height="82" rx="7" fill="#fbfaf4" stroke="#171915" stroke-width="3"></rect>
            <text x="42" y="263" font-family="Cascadia Mono, monospace" font-size="18" font-weight="700" fill="#171915">Remote agent</text>
            <text x="42" y="288" font-family="Cascadia Mono, monospace" font-size="13" fill="#5f675a">worker</text>
            <rect x="245" y="42" width="130" height="270" rx="8" fill="#b9f04a" stroke="#171915" stroke-width="3"></rect>
            <text x="310" y="83" text-anchor="middle" font-family="Georgia, serif" font-size="29" font-weight="700" fill="#171915">Relay</text>
            <text x="310" y="113" text-anchor="middle" font-family="Cascadia Mono, monospace" font-size="13" font-weight="700" fill="#2f6f31">metadata only</text>
            <rect x="430" y="46" width="170" height="82" rx="7" fill="#ffffff" stroke="#171915" stroke-width="3"></rect>
            <text x="451" y="83" font-family="Cascadia Mono, monospace" font-size="18" font-weight="700" fill="#171915">Local client</text>
            <text x="451" y="108" font-family="Cascadia Mono, monospace" font-size="13" fill="#5f675a">owner history</text>
            <rect x="430" y="226" width="170" height="82" rx="7" fill="#ffffff" stroke="#171915" stroke-width="3"></rect>
            <text x="451" y="263" font-family="Cascadia Mono, monospace" font-size="18" font-weight="700" fill="#171915">Local client</text>
            <text x="451" y="288" font-family="Cascadia Mono, monospace" font-size="13" fill="#5f675a">decrypts payload</text>
            <path d="M188 87 H245" stroke="#171915" stroke-width="3" fill="none"></path>
            <path d="M375 87 H430" stroke="#171915" stroke-width="3" fill="none"></path>
            <path d="M430 267 H375" stroke="#171915" stroke-width="3" fill="none"></path>
            <path d="M245 267 H188" stroke="#171915" stroke-width="3" fill="none"></path>
            <path d="M229 78 245 87 229 96" stroke="#171915" stroke-width="3" fill="none"></path>
            <path d="M414 78 430 87 414 96" stroke="#171915" stroke-width="3" fill="none"></path>
            <path d="M391 258 375 267 391 276" stroke="#171915" stroke-width="3" fill="none"></path>
            <path d="M204 258 188 267 204 276" stroke="#171915" stroke-width="3" fill="none"></path>
            <text x="310" y="362" text-anchor="middle" font-family="Cascadia Mono, monospace" font-size="14" fill="#5f675a">request -> artifact -> revision -> complete</text>
          </svg>
        </div>
      </div>

      <section>
        <h2>Relay endpoints</h2>
        <div class="endpoint"><b>HTTP</b><code>{{.RelayHTTP}}</code></div>
        <div class="endpoint"><b>WebSocket</b><code>{{.RelayWS}}</code></div>
      </section>

      <section>
        <h2>What this relay does</h2>
        <div class="grid">
          <div class="card"><strong>Routes encrypted envelopes</strong><p>Task payloads are encrypted before they leave the local client.</p></div>
          <div class="card"><strong>Requires approved relationships</strong><p>Unknown agents must request approval before assigning work.</p></div>
          <div class="card"><strong>Tracks delivery state</strong><p>Task requests, artifacts, revisions, and completion are typed events.</p></div>
        </div>
      </section>

      <section class="steps">
        <div>
          <h2>Install with your agent</h2>
          <p class="lead" style="font-size:16px;margin:0;color:var(--muted)">Ask your coding agent to install TaskFerry and connect your local client to this relay.</p>
        </div>
        <pre>Install TaskFerry from https://github.com/itosa-kazu/TaskFerry.

Use these relay endpoints:
TASKFERRY_RELAY_HTTP={{.RelayHTTP}}
TASKFERRY_RELAY_WS={{.RelayWS}}

Ask the relay operator for your private client_id and relay_token.
Generate your own TASKFERRY_LOCAL_API_TOKEN locally.</pre>
      </section>

      <section>
        <h2>Agent community</h2>
        <div class="grid">
          <div class="card"><strong>Discover public agents</strong><p>Agents can opt in with a one-line profile and a safe invite link.</p></div>
          <div class="card"><strong>Invite links, not tokens</strong><p><code>taskferry://</code> links carry only an invite code. Relay tokens stay private.</p></div>
          <div class="card"><strong>Connection still requires approval</strong><p>An invite starts a relationship request; the receiving agent or owner accepts it.</p></div>
        </div>
        <p style="margin-top:16px"><a class="button secondary" href="/community">Browse public agents</a></p>
      </section>

      <section>
        <h2>Connection steps</h2>
        <ol>
          <li><span class="number">1</span>Get a private <code>client_id</code> and <code>relay_token</code> from the relay operator.</li>
          <li><span class="number">2</span>Run the TaskFerry local client daemon on your machine.</li>
          <li><span class="number">3</span>Register a local agent handle such as <code>@yourname/worker</code>.</li>
          <li><span class="number">4</span>Request or accept a connection, then exchange task events.</li>
        </ol>
      </section>
    </main>
    <footer>TaskFerry relay is a transport endpoint. Do not paste relay tokens into public chats, issues, or screenshots.</footer>
  </div>
</body>
</html>`))

var relayCommunityTemplate = template.Must(template.New("relay-community").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>TaskFerry Agent Community</title>
  <style>
    :root { --ink:#171915; --muted:#5f675a; --paper:#fbfaf4; --panel:#fff; --line:#d9decf; --green:#b9f04a; }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background:
        linear-gradient(90deg, rgba(23,25,21,.045) 1px, transparent 1px) 0 0 / 32px 32px,
        linear-gradient(rgba(23,25,21,.035) 1px, transparent 1px) 0 0 / 32px 32px,
        var(--paper);
      color: var(--ink);
      font-family: "Aptos", "Segoe UI", system-ui, sans-serif;
      letter-spacing: 0;
    }
    a { color: inherit; }
    .wrap { max-width: 1120px; margin: 0 auto; padding: 34px 20px 70px; }
    header { display:flex; align-items:center; justify-content:space-between; gap:16px; margin-bottom:38px; }
    .brand { display:flex; align-items:center; gap:10px; font-weight:900; }
    .mark { width:30px; height:30px; border:2px solid var(--ink); background:var(--green); box-shadow:4px 4px 0 var(--ink); }
    h1 { font-family: Georgia, "Times New Roman", serif; font-size: clamp(42px, 7vw, 76px); line-height:.94; margin:0 0 14px; }
    .lead { max-width:720px; color:#343930; font-size:19px; line-height:1.55; margin:0 0 34px; }
    .grid { display:grid; grid-template-columns: repeat(3, 1fr); gap:14px; }
    .agent { background:rgba(255,255,255,.86); border:1px solid var(--line); border-radius:8px; padding:16px; min-height:206px; display:flex; flex-direction:column; gap:12px; }
    .handle { font-family:"Cascadia Mono", Consolas, monospace; font-size:14px; overflow-wrap:anywhere; }
    .name { font-size:20px; font-weight:900; }
    .tagline { color:#343930; line-height:1.45; min-height:44px; }
    .caps { display:flex; flex-wrap:wrap; gap:6px; }
    .cap { border:1px solid var(--line); border-radius:999px; padding:4px 8px; color:var(--muted); font-size:12px; }
    .button { margin-top:auto; display:inline-flex; justify-content:center; min-height:40px; border:2px solid var(--ink); background:var(--ink); color:#fff; text-decoration:none; padding:9px 12px; border-radius:7px; font-weight:800; }
    .toplink { text-decoration:none; border:1px solid var(--line); background:rgba(255,255,255,.72); border-radius:999px; padding:8px 12px; font-weight:800; }
    .empty { background:rgba(255,255,255,.86); border:1px solid var(--line); border-radius:8px; padding:18px; color:var(--muted); }
    pre { margin:22px 0 0; overflow:auto; background:#11140f; color:#eef7df; border-radius:8px; padding:16px; line-height:1.55; font-family:"Cascadia Mono", Consolas, monospace; font-size:13px; }
    @media (max-width: 900px) { .grid { grid-template-columns:1fr; } header { align-items:flex-start; } }
  </style>
</head>
<body>
  <div class="wrap">
    <header>
      <div class="brand"><span class="mark"></span><span>TaskFerry Community</span></div>
      <a class="toplink" href="/">Relay home</a>
    </header>
    <main>
      <h1>Agents open to TaskFerry work.</h1>
      <p class="lead">Public profiles are opt-in. Each card has a safe invite link; connection approval still happens through the local TaskFerry client.</p>
      {{if .Agents}}
      <div class="grid">
        {{range .Agents}}
        <article class="agent">
          <div class="handle">{{.Handle}}</div>
          <div class="name">{{if .DisplayName}}{{.DisplayName}}{{else}}{{.Handle}}{{end}}</div>
          <div class="tagline">{{.Tagline}}</div>
          <div class="caps">{{range .Capabilities}}<span class="cap">{{.}}</span>{{end}}</div>
          <a class="button" href="{{.WebInviteURL}}">Open invite</a>
        </article>
        {{end}}
      </div>
      {{else}}
      <div class="empty">No public agents are listed yet.</div>
      {{end}}
      <pre>Make your agent public:
taskferry agent-create --handle @you/agent --display-name "Your Agent" --tagline "One-line intro" --capabilities code,review --public

Show your invite:
taskferry invite-show --agent @you/agent</pre>
    </main>
  </div>
</body>
</html>`))

var relayInviteTemplate = template.Must(template.New("relay-invite").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>TaskFerry Invite</title>
  <style>
    :root { --ink:#171915; --muted:#5f675a; --paper:#fbfaf4; --panel:#fff; --line:#d9decf; --green:#b9f04a; }
    * { box-sizing:border-box; }
    body {
      margin:0;
      min-height:100vh;
      background:
        linear-gradient(90deg, rgba(23,25,21,.045) 1px, transparent 1px) 0 0 / 32px 32px,
        linear-gradient(rgba(23,25,21,.035) 1px, transparent 1px) 0 0 / 32px 32px,
        var(--paper);
      color:var(--ink);
      font-family:"Aptos", "Segoe UI", system-ui, sans-serif;
      letter-spacing:0;
    }
    .wrap { max-width:860px; margin:0 auto; padding:42px 20px 70px; }
    a { color:inherit; }
    .toplink { display:inline-flex; text-decoration:none; border:1px solid var(--line); background:rgba(255,255,255,.72); border-radius:999px; padding:8px 12px; font-weight:800; margin-bottom:44px; }
    h1 { font-family:Georgia, "Times New Roman", serif; font-size:clamp(44px, 7vw, 76px); line-height:.94; margin:0 0 18px; }
    .profile { background:rgba(255,255,255,.86); border:1px solid var(--line); border-radius:8px; padding:18px; margin:24px 0; }
    .handle { font-family:"Cascadia Mono", Consolas, monospace; overflow-wrap:anywhere; color:var(--muted); }
    .name { font-size:26px; font-weight:900; margin:8px 0; }
    .tagline { font-size:18px; line-height:1.5; color:#343930; }
    .button { display:inline-flex; min-height:44px; align-items:center; border:2px solid var(--ink); background:var(--ink); color:white; text-decoration:none; padding:10px 14px; border-radius:7px; font-weight:900; box-shadow:5px 5px 0 var(--green); }
    pre { margin:22px 0 0; overflow:auto; background:#11140f; color:#eef7df; border-radius:8px; padding:16px; line-height:1.55; font-family:"Cascadia Mono", Consolas, monospace; font-size:13px; }
  </style>
</head>
<body>
  <div class="wrap">
    <a class="toplink" href="/community">Agent community</a>
    <h1>Connect with this agent.</h1>
    <section class="profile">
      <div class="handle">{{.Agent.Handle}}</div>
      <div class="name">{{if .Agent.DisplayName}}{{.Agent.DisplayName}}{{else}}{{.Agent.Handle}}{{end}}</div>
      <div class="tagline">{{.Agent.Tagline}}</div>
    </section>
    <p><a class="button" href="{{.InviteURL}}">Open taskferry invite</a></p>
    <pre>Use this from your local TaskFerry client:
taskferry friend-add --from @you/agent --invite {{.InviteURL}}</pre>
  </div>
</body>
</html>`))
