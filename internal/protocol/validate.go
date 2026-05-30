package protocol

import (
	"errors"
	"regexp"
)

var handleRE = regexp.MustCompile(`^@[a-z0-9_-]+/[a-z0-9_-]+$`)

func ValidateHandle(handle string) error {
	if !handleRE.MatchString(handle) {
		return errors.New("invalid handle, expected @owner/agent_name")
	}
	return nil
}

func ValidateEnvelope(env Envelope) error {
	if env.ID == "" {
		return errors.New("missing message id")
	}
	if env.SchemaVersion == "" {
		return errors.New("missing schema version")
	}
	if env.Type == "" {
		return errors.New("missing message type")
	}
	if err := ValidateHandle(env.From); err != nil {
		return err
	}
	if len(env.To) == 0 {
		return errors.New("missing recipients")
	}
	for _, to := range env.To {
		if err := ValidateHandle(to); err != nil {
			return err
		}
	}
	if env.CreatedAt == "" {
		return errors.New("missing created_at")
	}
	if env.Payload.Mode == "" {
		return errors.New("missing payload")
	}
	return nil
}

func RequiresApprovedConnection(t MessageType) bool {
	switch t {
	case MessageTypeConnectionRequest, MessageTypeConnectionAccept:
		return false
	default:
		return true
	}
}

func PermissionAllows(t MessageType, p PermissionSet) bool {
	switch t {
	case MessageTypeMessage:
		return p.CanMessage
	case MessageTypeTaskRequest:
		return p.CanSendTask
	case MessageTypeArtifactSubmit:
		return p.CanSendArtifact
	case MessageTypeTaskAccept, MessageTypeTaskDecline, MessageTypeRevisionRequest, MessageTypeTaskComplete, MessageTypeTaskCancel:
		return p.CanMessage
	default:
		return true
	}
}
