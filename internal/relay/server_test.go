package relay

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/itosa-kazu/TaskFerry/internal/protocol"
)

func TestParseClientTokens(t *testing.T) {
	tokens, err := ParseClientTokens("client_a=tok_a, client_b = tok_b")
	if err != nil {
		t.Fatal(err)
	}
	if tokens["client_a"] != "tok_a" {
		t.Fatalf("client_a token = %q", tokens["client_a"])
	}
	if tokens["client_b"] != "tok_b" {
		t.Fatalf("client_b token = %q", tokens["client_b"])
	}
}

func TestParseClientTokensRejectsInvalidEntries(t *testing.T) {
	if _, err := ParseClientTokens("client_a"); err == nil {
		t.Fatal("expected invalid token mapping error")
	}
}

func TestTokenOKPrefersPerClientTokens(t *testing.T) {
	s := NewServer(nil, AuthConfig{
		GlobalToken: "global",
		ClientTokens: map[string]string{
			"client_a": "tok_a",
		},
	})
	if !s.tokenOK("client_a", "Bearer tok_a") {
		t.Fatal("expected per-client token to pass")
	}
	if !s.tokenOK("unknown", "global") {
		t.Fatal("expected global token fallback to pass")
	}
	if s.tokenOK("client_a", "wrong") {
		t.Fatal("expected wrong token to fail")
	}
}

func TestTokenOKAcceptsSelfSignupClient(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cred, err := store.CreateClient("Alice", "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(store, AuthConfig{})
	if !s.tokenOK(cred.ClientID, cred.Token) {
		t.Fatal("expected database client token to pass")
	}
	if s.tokenOK(cred.ClientID, "wrong") {
		t.Fatal("expected wrong database token to fail")
	}
}

func TestSignupCreatesClientCredential(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cred, err := store.CreateClient("Alice", "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if cred.ClientID == "" || cred.Token == "" {
		t.Fatalf("missing credential fields: %+v", cred)
	}
	token, err := store.ClientToken(cred.ClientID)
	if err != nil {
		t.Fatal(err)
	}
	if token != cred.Token {
		t.Fatalf("stored token mismatch")
	}
}

func TestNormalizeSignupRequiresValidEmail(t *testing.T) {
	owner, email, err := normalizeSignup("", "ALICE@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if owner != "alice" || email != "alice@example.com" {
		t.Fatalf("normalized signup = owner %q email %q", owner, email)
	}
	if _, _, err := normalizeSignup("Alice", "not-an-email"); err == nil {
		t.Fatal("expected invalid email error")
	}
}

func TestSignupAPIRateLimitsByIP(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := NewServer(store, AuthConfig{SignupLimitPerHour: 1})

	first := httptest.NewRequest(http.MethodPost, "/v1/signup", strings.NewReader(`{"owner_name":"Alice","email":"alice@example.com"}`))
	first.RemoteAddr = "203.0.113.10:1234"
	firstRecorder := httptest.NewRecorder()
	s.handleSignupAPI(firstRecorder, first)
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first signup status = %d body = %s", firstRecorder.Code, firstRecorder.Body.String())
	}

	second := httptest.NewRequest(http.MethodPost, "/v1/signup", strings.NewReader(`{"owner_name":"Bob","email":"bob@example.com"}`))
	second.RemoteAddr = "203.0.113.10:5678"
	secondRecorder := httptest.NewRecorder()
	s.handleSignupAPI(secondRecorder, second)
	if secondRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second signup status = %d body = %s", secondRecorder.Code, secondRecorder.Body.String())
	}
}

func TestHomePageLinksSignup(t *testing.T) {
	s := NewServer(nil, AuthConfig{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()
	s.handleHome(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("home status = %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `href="/signup">Create account`) {
		t.Fatalf("home page does not expose signup entry")
	}
}

func TestSignupPageRendersCopyControls(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := NewServer(store, AuthConfig{})

	req := httptest.NewRequest(http.MethodPost, "/signup", strings.NewReader("owner_name=Alice&email=alice%40example.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.11:1234"
	recorder := httptest.NewRecorder()
	s.handleSignupPage(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("signup status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, expected := range []string{`data-copy="#client-id"`, `data-copy="#relay-token"`, `data-copy="#config-block"`, `<textarea id="config-block"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("signup page missing %q", expected)
		}
	}
	if !strings.Contains(body, "This signup created a private relay account") {
		t.Fatalf("signup page does not explain public profile next step")
	}
}

func TestCommunityEmptyStateExplainsPublicAgentRequirement(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := NewServer(store, AuthConfig{})

	req := httptest.NewRequest(http.MethodGet, "/community", nil)
	recorder := httptest.NewRecorder()
	s.handleCommunity(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("community status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "Creating a relay account does not publish an agent profile") {
		t.Fatalf("community empty state does not explain account/profile separation")
	}
}

func TestStoreCreatesInvitesAndPublicDirectory(t *testing.T) {
	store, err := OpenStore(filepath.Join(t.TempDir(), "relay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	publicAgent := protocol.AgentProfile{
		AgentID:       "agent_public",
		Handle:        "@alice/worker",
		DisplayName:   "Alice Worker",
		Description:   "Longer profile",
		Tagline:       "Writes short technical drafts",
		Capabilities:  []string{"writing", "review"},
		PublicProfile: true,
	}
	privateAgent := protocol.AgentProfile{
		AgentID:       "agent_private",
		Handle:        "@bob/private",
		DisplayName:   "Bob Private",
		Tagline:       "Private worker",
		PublicProfile: false,
	}
	if err := store.UpsertAgent("client_a", "device_a", publicAgent); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertAgent("client_b", "device_b", privateAgent); err != nil {
		t.Fatal(err)
	}

	invite, err := store.InviteByHandle("@alice/worker")
	if err != nil {
		t.Fatal(err)
	}
	if invite.Code == "" {
		t.Fatal("expected invite code")
	}
	resolved, err := store.Invite(invite.Code)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Handle != "@alice/worker" {
		t.Fatalf("resolved handle = %q", resolved.Handle)
	}

	agents, err := store.PublicAgents(20, "https://relay.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("public agents count = %d", len(agents))
	}
	if agents[0].Handle != "@alice/worker" || agents[0].InviteURL == "" || agents[0].WebInviteURL == "" {
		t.Fatalf("unexpected directory agent: %+v", agents[0])
	}
}
