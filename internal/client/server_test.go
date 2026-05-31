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
	agent, err := store.Agent("@alice/agent")
	if err != nil {
		t.Fatal(err)
	}
	if !agent.PublicProfile || agent.Tagline != "Reviews code" {
		t.Fatalf("agent mismatch: %+v", agent)
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
