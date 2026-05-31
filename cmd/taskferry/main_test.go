package main

import (
	"strings"
	"testing"

	"github.com/itosa-kazu/TaskFerry/internal/localapi"
)

func TestLocalSetupURLConvertsTaskFerrySetupLink(t *testing.T) {
	c := localapi.New("http://127.0.0.1:4318", "local-token")
	target, err := localSetupURL(c, "taskferry://relay.example.com/setup?client_id=client_x&relay_token=relay_y")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"http://127.0.0.1:4318/setup?",
		"client_id=client_x",
		"relay_token=relay_y",
		"relay_http=https%3A%2F%2Frelay.example.com",
		"relay_ws=wss%3A%2F%2Frelay.example.com%2Fv1%2Fws",
		"token=local-token",
	} {
		if !strings.Contains(target, expected) {
			t.Fatalf("target missing %q: %s", expected, target)
		}
	}
}

func TestIsSetupLinkRejectsInvite(t *testing.T) {
	if isSetupLink("taskferry://relay.example.com/invite/inv_x") {
		t.Fatal("invite link should not be treated as setup")
	}
}
