package relay

import "testing"

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
