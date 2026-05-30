package protocol

import (
	"encoding/json"
	"testing"
)

func TestEncryptDecryptAndSignEnvelope(t *testing.T) {
	signPub, signPriv, err := GenerateSigningKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	encPub, encPriv, err := GenerateEncryptionKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"text": "hello"}
	encrypted, err := EncryptPayloadJSON(payload, encPub)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := DecryptPayload(encrypted, encPriv, encPub)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(plain, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["text"] != "hello" {
		t.Fatalf("unexpected payload: %#v", decoded)
	}
	env := NewEnvelope(MessageTypeMessage, "@alice/requester", []string{"@bob/writer"}, encrypted)
	if err := SignEnvelope(&env, signPriv); err != nil {
		t.Fatal(err)
	}
	if err := VerifyEnvelopeSignature(env, signPub); err != nil {
		t.Fatal(err)
	}
	env.Type = MessageTypeTaskRequest
	if err := VerifyEnvelopeSignature(env, signPub); err == nil {
		t.Fatal("expected modified envelope signature to fail")
	}
}
