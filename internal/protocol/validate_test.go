package protocol

import "testing"

func TestValidateHandle(t *testing.T) {
	valid := []string{"@alice/requester", "@wenyi1000/jp-lqa", "@team_name/agent-1"}
	for _, handle := range valid {
		if err := ValidateHandle(handle); err != nil {
			t.Fatalf("expected %s to be valid: %v", handle, err)
		}
	}
	invalid := []string{"alice/requester", "@Alice/requester", "@alice", "@alice/requester/extra", "@alice/agent.name"}
	for _, handle := range invalid {
		if err := ValidateHandle(handle); err == nil {
			t.Fatalf("expected %s to be invalid", handle)
		}
	}
}

func TestPermissionAllows(t *testing.T) {
	perm := DefaultConnectionPermissions()
	if !PermissionAllows(MessageTypeTaskRequest, perm) {
		t.Fatal("default connection should allow task requests")
	}
	perm.CanSendTask = false
	if PermissionAllows(MessageTypeTaskRequest, perm) {
		t.Fatal("disabled task permission should block task requests")
	}
	if RequiresApprovedConnection(MessageTypeConnectionRequest) {
		t.Fatal("connection requests should be allowed before approval")
	}
}
