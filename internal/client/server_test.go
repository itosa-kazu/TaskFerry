package client

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itosa-kazu/TaskFerry/internal/protocol"
)

func TestSetupPageAppliesRelayConfigAndCreatesPublicAgent(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "client.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/agents/register":
			writeJSON(w, http.StatusOK, protocol.RegisterAgentResponse{OK: true})
		case "/v1/agents/invite":
			writeJSON(w, http.StatusOK, protocol.InviteResponse{
				OK:           true,
				InviteURL:    "taskferry://relay.example.com/invite/inv_test",
				WebInviteURL: "https://relay.example.com/invite/inv_test",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer relay.Close()

	s := NewServer(Config{
		ClientID:      "client_dev",
		DeviceID:      "device_dev",
		OwnerID:       "owner_dev",
		RelayHTTP:     "http://127.0.0.1:1",
		RelayWS:       "ws://127.0.0.1:1/v1/ws",
		LocalAPIToken: "local-token",
	}, store)

	form := url.Values{}
	form.Set("client_id", "client_signup")
	form.Set("relay_token", "relay_secret")
	form.Set("relay_http", relay.URL)
	form.Set("relay_ws", strings.Replace(relay.URL, "http://", "ws://", 1)+"/v1/ws")
	form.Set("token", "local-token")
	form.Set("handle", "@alice/agent")
	form.Set("display_name", "Alice Agent")
	form.Set("tagline", "Reviews code")
	form.Set("capabilities", "code,review")
	form.Set("public_profile", "on")
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleSetupPage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Local setup saved") || !strings.Contains(rec.Body.String(), "taskferry://relay.example.com/invite/inv_test") {
		t.Fatalf("setup success page missing expected content: %s", rec.Body.String())
	}

	cfg := s.config()
	if cfg.ClientID != "client_signup" || cfg.RelayToken != "relay_secret" || cfg.DeviceID == "device_dev" {
		t.Fatalf("config not applied: %+v", cfg)
	}
	saved, err := store.SavedRelayConfig()
	if err != nil {
		t.Fatal(err)
	}
	if saved.ClientID != "client_signup" || saved.RelayHTTP != relay.URL {
		t.Fatalf("saved config mismatch: %+v", saved)
	}
	var storedRelayToken string
	if err := store.db.QueryRow(`SELECT value FROM settings WHERE key = 'relay_token'`).Scan(&storedRelayToken); err != nil {
		t.Fatal(err)
	}
	if storedRelayToken == "relay_secret" {
		t.Fatal("relay token was stored in plaintext")
	}
	agent, err := store.Agent("@alice/agent")
	if err != nil {
		t.Fatal(err)
	}
	if !agent.PublicProfile || agent.Tagline != "Reviews code" {
		t.Fatalf("agent mismatch: %+v", agent)
	}
	var storedSigningPrivateKey string
	if err := store.db.QueryRow(`SELECT signing_private_key FROM agents WHERE handle = ?`, "@alice/agent").Scan(&storedSigningPrivateKey); err != nil {
		t.Fatal(err)
	}
	if storedSigningPrivateKey == agent.SigningPrivateKey {
		t.Fatal("agent signing private key was stored in plaintext")
	}
}

func TestSetupPageSuggestsHandleFromOwnerName(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "client.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := NewServer(Config{}, store)
	req := httptest.NewRequest(http.MethodGet, "/setup?owner_name=Alice+Example&client_id=client_x", nil)
	rec := httptest.NewRecorder()
	s.handleSetupPage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `value="@aliceexample/agent"`) {
		t.Fatalf("setup page did not suggest handle from owner name: %s", rec.Body.String())
	}
}

func TestConnectPageCreatesDefaultAgentAndSendsRequest(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "client.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	signPub, _, err := protocol.GenerateSigningKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	encPub, _, err := protocol.GenerateEncryptionKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	remote := protocol.AgentProfile{
		AgentID:             "agent_remote",
		Handle:              "@remote/agent",
		DisplayName:         "Remote Agent",
		Tagline:             "Accepts review tasks",
		SigningPublicKey:    signPub,
		EncryptionPublicKey: encPub,
		PublicProfile:       true,
	}
	directoryRemote := protocol.DirectoryAgent{
		Handle:        remote.Handle,
		DisplayName:   remote.DisplayName,
		Tagline:       remote.Tagline,
		InviteCode:    "inv_test",
		PublicProfile: true,
	}
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/invites/inv_test":
			writeJSON(w, http.StatusOK, protocol.InviteResponse{OK: true, InviteCode: "inv_test", Agent: &directoryRemote})
		case "/v1/agents/resolve":
			writeJSON(w, http.StatusOK, protocol.ResolveAgentResponse{OK: true, Agent: &remote})
		case "/v1/agents/register":
			writeJSON(w, http.StatusOK, protocol.RegisterAgentResponse{OK: true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer relay.Close()

	s := NewServer(Config{
		ClientID:      "client_local",
		DeviceID:      "device_local",
		OwnerID:       "client_local",
		RelayHTTP:     relay.URL,
		RelayWS:       strings.Replace(relay.URL, "http://", "ws://", 1) + "/v1/ws",
		RelayToken:    "relay_secret",
		LocalAPIToken: "local-token",
	}, store)

	req := httptest.NewRequest(http.MethodGet, "/connect?invite=inv_test&token=local-token", nil)
	rec := httptest.NewRecorder()
	s.handleConnectPage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("connect status = %d body = %s", rec.Code, rec.Body.String())
	}
	for _, expected := range []string{"Create your TaskFerry agent", `name="create_handle"`, "Create agent and send request"} {
		if !strings.Contains(rec.Body.String(), expected) {
			t.Fatalf("connect page missing %q: %s", expected, rec.Body.String())
		}
	}

	form := url.Values{}
	form.Set("invite", "inv_test")
	form.Set("token", "local-token")
	form.Set("create_handle", "@local/agent")
	form.Set("create_display_name", "Local Agent")
	form.Set("create_tagline", "Ready to work")
	form.Set("create_capabilities", "code,review")
	form.Set("message", "Please connect.")
	post := httptest.NewRequest(http.MethodPost, "/connect", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRec := httptest.NewRecorder()
	s.handleConnectPage(postRec, post)
	if postRec.Code != http.StatusOK {
		t.Fatalf("connect post status = %d body = %s", postRec.Code, postRec.Body.String())
	}
	if !strings.Contains(postRec.Body.String(), "Request sent") || !strings.Contains(postRec.Body.String(), "@local/agent") {
		t.Fatalf("connect success missing expected content: %s", postRec.Body.String())
	}
	agent, err := store.Agent("@local/agent")
	if err != nil {
		t.Fatal(err)
	}
	if agent.DisplayName != "Local Agent" || agent.Tagline != "Ready to work" {
		t.Fatalf("created agent mismatch: %+v", agent)
	}
}
