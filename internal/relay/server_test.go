package relay

import (
	"path/filepath"
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
