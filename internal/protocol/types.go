package protocol

import "time"

const SchemaVersion = "0.1"

type MessageType string

const (
	MessageTypeMessage           MessageType = "message"
	MessageTypeConnectionRequest MessageType = "connection_request"
	MessageTypeConnectionAccept  MessageType = "connection_accept"
	MessageTypeTaskRequest       MessageType = "task_request"
	MessageTypeTaskAccept        MessageType = "task_accept"
	MessageTypeTaskDecline       MessageType = "task_decline"
	MessageTypeArtifactSubmit    MessageType = "artifact_submit"
	MessageTypeRevisionRequest   MessageType = "revision_request"
	MessageTypeTaskComplete      MessageType = "task_complete"
	MessageTypeTaskCancel        MessageType = "task_cancel"
)

type TaskStatus string

const (
	TaskStatusCreated           TaskStatus = "created"
	TaskStatusSent              TaskStatus = "sent"
	TaskStatusAccepted          TaskStatus = "accepted"
	TaskStatusDeclined          TaskStatus = "declined"
	TaskStatusWorking           TaskStatus = "working"
	TaskStatusSubmitted         TaskStatus = "submitted"
	TaskStatusRevisionRequested TaskStatus = "revision_requested"
	TaskStatusResubmitted       TaskStatus = "resubmitted"
	TaskStatusCompleted         TaskStatus = "completed"
	TaskStatusCancelled         TaskStatus = "cancelled"
	TaskStatusExpired           TaskStatus = "expired"
	TaskStatusFailed            TaskStatus = "failed"
)

type DeliveryState string

const (
	DeliveryPending           DeliveryState = "pending"
	DeliveryRelayAccepted     DeliveryState = "relay_accepted"
	DeliveryDeliveredToClient DeliveryState = "delivered_to_client"
	DeliveryFailed            DeliveryState = "failed"
)

type ProcessingState string

const (
	ProcessingUnread    ProcessingState = "unread"
	ProcessingRead      ProcessingState = "read"
	ProcessingProcessed ProcessingState = "processed"
)

type PermissionSet struct {
	CanMessage       bool `json:"can_message"`
	CanSendTask      bool `json:"can_send_task"`
	CanSendArtifact  bool `json:"can_send_artifact"`
	CanAutoTrigger   bool `json:"can_auto_trigger"`
	MaxRatePerMinute int  `json:"max_rate_per_minute"`
	MaxTaskBudget    int  `json:"max_task_budget"`
	RequireHumanGate bool `json:"require_human_gate"`
}

func DefaultConnectionPermissions() PermissionSet {
	return PermissionSet{
		CanMessage:       true,
		CanSendTask:      true,
		CanSendArtifact:  true,
		CanAutoTrigger:   false,
		MaxRatePerMinute: 60,
		MaxTaskBudget:    20,
		RequireHumanGate: true,
	}
}

type AgentProfile struct {
	AgentID             string   `json:"agent_id"`
	Handle              string   `json:"handle"`
	OwnerID             string   `json:"owner_id"`
	DeviceID            string   `json:"device_id"`
	DisplayName         string   `json:"display_name"`
	Description         string   `json:"description"`
	Capabilities        []string `json:"capabilities"`
	SigningPublicKey    string   `json:"signing_public_key"`
	EncryptionPublicKey string   `json:"encryption_public_key"`
	CreatedAt           string   `json:"created_at,omitempty"`
	UpdatedAt           string   `json:"updated_at,omitempty"`
}

type EncryptedPayload struct {
	Mode               string `json:"mode"`
	Algorithm          string `json:"algorithm"`
	ContentType        string `json:"content_type"`
	EphemeralPublicKey string `json:"ephemeral_public_key"`
	Nonce              string `json:"nonce"`
	Ciphertext         string `json:"ciphertext"`
}

type Envelope struct {
	ID             string            `json:"id"`
	SchemaVersion  string            `json:"schema_version"`
	Type           MessageType       `json:"type"`
	From           string            `json:"from"`
	To             []string          `json:"to"`
	ConversationID string            `json:"conversation_id,omitempty"`
	TaskID         string            `json:"task_id,omitempty"`
	ReplyTo        string            `json:"reply_to,omitempty"`
	CreatedAt      string            `json:"created_at"`
	Payload        EncryptedPayload  `json:"payload"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	SigningKeyID   string            `json:"signing_key_id,omitempty"`
	Signature      string            `json:"signature,omitempty"`
}

type RegisterAgentRequest struct {
	ClientID string       `json:"client_id"`
	DeviceID string       `json:"device_id"`
	Agent    AgentProfile `json:"agent"`
}

type RegisterAgentResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type ResolveAgentResponse struct {
	OK    bool          `json:"ok"`
	Error string        `json:"error,omitempty"`
	Agent *AgentProfile `json:"agent,omitempty"`
}

type RelayFrame struct {
	Kind      string    `json:"kind"`
	Envelope  *Envelope `json:"envelope,omitempty"`
	MessageID string    `json:"message_id,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type SendMessageRequest struct {
	From    string      `json:"from"`
	To      []string    `json:"to"`
	Type    MessageType `json:"type"`
	Payload any         `json:"payload"`
}

type CreateAgentRequest struct {
	Handle       string   `json:"handle"`
	DisplayName  string   `json:"display_name"`
	Description  string   `json:"description"`
	Capabilities []string `json:"capabilities"`
	OwnerID      string   `json:"owner_id,omitempty"`
}

type TaskRequestPayload struct {
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Requirements   []string `json:"requirements,omitempty"`
	MaxRevisions   int      `json:"max_revisions"`
	ExpectedFormat string   `json:"expected_format,omitempty"`
}

type TaskDecisionPayload struct {
	Message string `json:"message,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type ArtifactSubmitPayload struct {
	Version      int    `json:"version"`
	ArtifactType string `json:"artifact_type"`
	Content      any    `json:"content"`
	Notes        string `json:"notes,omitempty"`
}

type RevisionRequestPayload struct {
	Reason             string   `json:"reason"`
	RequestedChanges   []string `json:"requested_changes,omitempty"`
	RemainingRevisions int      `json:"remaining_revisions,omitempty"`
}

func NewEnvelope(messageType MessageType, from string, to []string, payload EncryptedPayload) Envelope {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return Envelope{
		ID:            NewID("msg"),
		SchemaVersion: SchemaVersion,
		Type:          messageType,
		From:          from,
		To:            to,
		CreatedAt:     now,
		Payload:       payload,
		Metadata:      map[string]string{},
	}
}
