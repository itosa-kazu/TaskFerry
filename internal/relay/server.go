package relay

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
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
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /v1/agents/register", s.handleRegisterAgent)
	mux.HandleFunc("GET /v1/agents/resolve", s.handleResolveAgent)
	mux.HandleFunc("GET /v1/ws", s.handleWebSocket)
	return s.withCORS(mux)
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
