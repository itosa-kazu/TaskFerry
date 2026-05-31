package secretstore

import "testing"

func TestProtectRoundTrip(t *testing.T) {
	protected, err := Protect("test", "secret-value")
	if err != nil {
		t.Fatal(err)
	}
	if protected == "secret-value" {
		t.Fatal("expected protected value to differ from plaintext")
	}
	plain, err := Unprotect(protected)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "secret-value" {
		t.Fatalf("unprotected value = %q", plain)
	}
}

func TestUnprotectPlaintextCompatibility(t *testing.T) {
	plain, err := Unprotect("legacy-secret")
	if err != nil {
		t.Fatal(err)
	}
	if plain != "legacy-secret" {
		t.Fatalf("plain value = %q", plain)
	}
}
