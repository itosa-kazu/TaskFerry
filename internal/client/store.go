package client

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/itosa-kazu/TaskFerry/internal/protocol"
	"github.com/itosa-kazu/TaskFerry/internal/secretstore"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type AgentRecord struct {
	AgentID              string
	Handle               string
	OwnerID              string
	DeviceID             string
	DisplayName          string
	Description          string
	Tagline              string
	Capabilities         []string
	PublicProfile        bool
	SigningPublicKey     string
	SigningPrivateKey    string
	EncryptionPublicKey  string
	EncryptionPrivateKey string
	CreatedAt            string
	UpdatedAt            string
}

type LocalMessage struct {
	MessageID       string
	ConversationID  string
	TaskID          string
	Type            protocol.MessageType
	Sender          string
	Recipients      []string
	Direction       string
	DeliveryState   protocol.DeliveryState
	ProcessingState protocol.ProcessingState
	Plaintext       json.RawMessage
	CreatedAt       string
	ReceivedAt      string
}

type TaskRecord struct {
	TaskID         string
	ConversationID string
	Creator        string
	Assignee       string
	Title          string
	Description    string
	Status         protocol.TaskStatus
	MaxRevisions   int
	RevisionCount  int
	UpdatedAt      string
}

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	stmts := []string{
		`PRAGMA journal_mode = WAL`,
		`CREATE TABLE IF NOT EXISTS agents (
			handle TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			owner_id TEXT NOT NULL,
			device_id TEXT NOT NULL,
			display_name TEXT NOT NULL,
			description TEXT NOT NULL,
			capabilities_json TEXT NOT NULL,
			signing_public_key TEXT NOT NULL,
			signing_private_key TEXT NOT NULL,
			encryption_public_key TEXT NOT NULL,
			encryption_private_key TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS messages (
			message_id TEXT PRIMARY KEY,
			conversation_id TEXT,
			task_id TEXT,
			message_type TEXT NOT NULL,
			sender TEXT NOT NULL,
			recipients_json TEXT NOT NULL,
			direction TEXT NOT NULL,
			delivery_state TEXT NOT NULL,
			processing_state TEXT NOT NULL,
			envelope_json TEXT NOT NULL,
			plaintext_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			received_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_recipient_processing ON messages(direction, processing_state, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_task ON messages(task_id, created_at)`,
		`CREATE TABLE IF NOT EXISTS tasks (
			task_id TEXT PRIMARY KEY,
			conversation_id TEXT,
			creator TEXT NOT NULL,
			assignee TEXT NOT NULL,
			title TEXT NOT NULL,
			description TEXT NOT NULL,
			status TEXT NOT NULL,
			max_revisions INTEGER NOT NULL DEFAULT 0,
			revision_count INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			completed_at TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			level TEXT NOT NULL,
			event_type TEXT NOT NULL,
			message TEXT NOT NULL,
			data_json TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	if err := s.ensureColumn("agents", "tagline", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn("agents", "public_profile", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	return nil
}

func (s *Store) SavedRelayConfig() (Config, error) {
	rows, err := s.db.Query(`SELECT key, value FROM settings WHERE key IN ('client_id', 'device_id', 'owner_id', 'relay_http', 'relay_ws', 'relay_token')`)
	if err != nil {
		return Config{}, err
	}
	defer rows.Close()
	cfg := Config{}
	for rows.Next() {
		var key string
		var value string
		if err := rows.Scan(&key, &value); err != nil {
			return Config{}, err
		}
		switch key {
		case "client_id":
			cfg.ClientID = value
		case "device_id":
			cfg.DeviceID = value
		case "owner_id":
			cfg.OwnerID = value
		case "relay_http":
			cfg.RelayHTTP = value
		case "relay_ws":
			cfg.RelayWS = value
		case "relay_token":
			cfg.RelayToken, err = secretstore.Unprotect(value)
			if err != nil {
				return Config{}, err
			}
		}
	}
	return cfg, rows.Err()
}

func (s *Store) SaveRelayConfig(cfg Config) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	relayToken, err := secretstore.Protect("relay_token", cfg.RelayToken)
	if err != nil {
		return err
	}
	values := map[string]string{
		"client_id":   cfg.ClientID,
		"device_id":   cfg.DeviceID,
		"owner_id":    cfg.OwnerID,
		"relay_http":  cfg.RelayHTTP,
		"relay_ws":    cfg.RelayWS,
		"relay_token": relayToken,
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, value := range values {
		if value == "" {
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO settings(key, value, updated_at) VALUES(?, ?, ?)
			 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
			key, value, now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ensureColumn(table string, column string, spec string) error {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + spec)
	return err
}

func (s *Store) CreateAgent(handle string, ownerID string, deviceID string, displayName string, description string, tagline string, capabilities []string, publicProfile bool) (AgentRecord, error) {
	if err := protocol.ValidateHandle(handle); err != nil {
		return AgentRecord{}, err
	}
	if existing, err := s.Agent(handle); err == nil {
		existing.DisplayName = displayName
		existing.Description = description
		existing.Tagline = tagline
		existing.Capabilities = capabilities
		existing.PublicProfile = publicProfile
		existing.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := s.UpsertAgent(existing); err != nil {
			return AgentRecord{}, err
		}
		return existing, nil
	}
	signPub, signPriv, err := protocol.GenerateSigningKeyPair()
	if err != nil {
		return AgentRecord{}, err
	}
	encPub, encPriv, err := protocol.GenerateEncryptionKeyPair()
	if err != nil {
		return AgentRecord{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rec := AgentRecord{
		AgentID:              protocol.NewID("agent"),
		Handle:               handle,
		OwnerID:              ownerID,
		DeviceID:             deviceID,
		DisplayName:          displayName,
		Description:          description,
		Tagline:              tagline,
		Capabilities:         capabilities,
		PublicProfile:        publicProfile,
		SigningPublicKey:     signPub,
		SigningPrivateKey:    signPriv,
		EncryptionPublicKey:  encPub,
		EncryptionPrivateKey: encPriv,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if err := s.UpsertAgent(rec); err != nil {
		return AgentRecord{}, err
	}
	return rec, nil
}

func (s *Store) UpsertAgent(rec AgentRecord) error {
	caps, err := json.Marshal(rec.Capabilities)
	if err != nil {
		return err
	}
	signingPrivateKey, err := secretstore.Protect("agent_signing_private_key:"+rec.Handle, rec.SigningPrivateKey)
	if err != nil {
		return err
	}
	encryptionPrivateKey, err := secretstore.Protect("agent_encryption_private_key:"+rec.Handle, rec.EncryptionPrivateKey)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO agents(handle, agent_id, owner_id, device_id, display_name, description, tagline, capabilities_json, public_profile, signing_public_key, signing_private_key, encryption_public_key, encryption_private_key, created_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(handle) DO UPDATE SET
		  display_name=excluded.display_name,
		  description=excluded.description,
		  tagline=excluded.tagline,
		  capabilities_json=excluded.capabilities_json,
		  public_profile=excluded.public_profile,
		  updated_at=excluded.updated_at`,
		rec.Handle, rec.AgentID, rec.OwnerID, rec.DeviceID, rec.DisplayName, rec.Description, rec.Tagline, string(caps), boolInt(rec.PublicProfile),
		rec.SigningPublicKey, signingPrivateKey, rec.EncryptionPublicKey, encryptionPrivateKey,
		rec.CreatedAt, time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *Store) Agent(handle string) (AgentRecord, error) {
	var rec AgentRecord
	var caps string
	var publicProfile int
	err := s.db.QueryRow(
		`SELECT agent_id, handle, owner_id, device_id, display_name, description, tagline, capabilities_json, public_profile, signing_public_key, signing_private_key, encryption_public_key, encryption_private_key, created_at, updated_at
		 FROM agents WHERE handle = ?`,
		handle,
	).Scan(&rec.AgentID, &rec.Handle, &rec.OwnerID, &rec.DeviceID, &rec.DisplayName, &rec.Description, &rec.Tagline, &caps, &publicProfile,
		&rec.SigningPublicKey, &rec.SigningPrivateKey, &rec.EncryptionPublicKey, &rec.EncryptionPrivateKey, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AgentRecord{}, ErrNotFound
		}
		return AgentRecord{}, err
	}
	if err := json.Unmarshal([]byte(caps), &rec.Capabilities); err != nil {
		return AgentRecord{}, err
	}
	rec.SigningPrivateKey, err = secretstore.Unprotect(rec.SigningPrivateKey)
	if err != nil {
		return AgentRecord{}, err
	}
	rec.EncryptionPrivateKey, err = secretstore.Unprotect(rec.EncryptionPrivateKey)
	if err != nil {
		return AgentRecord{}, err
	}
	rec.PublicProfile = publicProfile != 0
	return rec, nil
}

func (s *Store) Agents() ([]AgentRecord, error) {
	rows, err := s.db.Query(`SELECT handle FROM agents ORDER BY handle`)
	if err != nil {
		return nil, err
	}
	var handles []string
	for rows.Next() {
		var handle string
		if err := rows.Scan(&handle); err != nil {
			_ = rows.Close()
			return nil, err
		}
		handles = append(handles, handle)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]AgentRecord, 0, len(handles))
	for _, handle := range handles {
		rec, err := s.Agent(handle)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func (a AgentRecord) Profile() protocol.AgentProfile {
	return protocol.AgentProfile{
		AgentID:             a.AgentID,
		Handle:              a.Handle,
		OwnerID:             a.OwnerID,
		DeviceID:            a.DeviceID,
		DisplayName:         a.DisplayName,
		Description:         a.Description,
		Tagline:             a.Tagline,
		Capabilities:        a.Capabilities,
		PublicProfile:       a.PublicProfile,
		SigningPublicKey:    a.SigningPublicKey,
		EncryptionPublicKey: a.EncryptionPublicKey,
		CreatedAt:           a.CreatedAt,
		UpdatedAt:           a.UpdatedAt,
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s *Store) SaveMessage(env protocol.Envelope, direction string, plaintext []byte, delivery protocol.DeliveryState, processing protocol.ProcessingState) error {
	recipients, err := json.Marshal(env.To)
	if err != nil {
		return err
	}
	envelope, err := json.Marshal(env)
	if err != nil {
		return err
	}
	receivedAt := ""
	if direction == "inbound" {
		receivedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	_, err = s.db.Exec(
		`INSERT INTO messages(message_id, conversation_id, task_id, message_type, sender, recipients_json, direction, delivery_state, processing_state, envelope_json, plaintext_json, created_at, received_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(message_id) DO UPDATE SET
		 	delivery_state=excluded.delivery_state,
		 	processing_state=CASE WHEN messages.processing_state = 'processed' THEN messages.processing_state ELSE excluded.processing_state END`,
		env.ID, env.ConversationID, env.TaskID, env.Type, env.From, string(recipients), direction, delivery, processing,
		string(envelope), string(plaintext), env.CreatedAt, receivedAt,
	)
	return err
}

func (s *Store) MarkDelivery(messageID string, delivery protocol.DeliveryState) error {
	_, err := s.db.Exec(`UPDATE messages SET delivery_state = ? WHERE message_id = ?`, delivery, messageID)
	return err
}

func (s *Store) MarkProcessed(messageID string) error {
	_, err := s.db.Exec(`UPDATE messages SET processing_state = ? WHERE message_id = ?`, protocol.ProcessingProcessed, messageID)
	return err
}

func (s *Store) Inbox(agentID string, limit int, unprocessedOnly bool) ([]LocalMessage, error) {
	query := `SELECT message_id, conversation_id, task_id, message_type, sender, recipients_json, direction, delivery_state, processing_state, plaintext_json, created_at, received_at
		FROM messages
		WHERE direction = 'inbound' AND recipients_json LIKE ?`
	args := []any{"%" + agentID + "%"}
	if unprocessedOnly {
		query += ` AND processing_state != 'processed'`
	}
	query += ` ORDER BY created_at LIMIT ?`
	args = append(args, limit)
	return s.queryMessages(query, args...)
}

func (s *Store) RecentMessages(limit int) ([]LocalMessage, error) {
	return s.queryMessages(`SELECT message_id, conversation_id, task_id, message_type, sender, recipients_json, direction, delivery_state, processing_state, plaintext_json, created_at, received_at FROM messages ORDER BY created_at DESC LIMIT ?`, limit)
}

func (s *Store) PendingOutbound(limit int) ([]protocol.Envelope, error) {
	rows, err := s.db.Query(`SELECT envelope_json FROM messages WHERE direction = 'outbound' AND delivery_state = ? ORDER BY created_at LIMIT ?`, protocol.DeliveryPending, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.Envelope
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var env protocol.Envelope
		if err := json.Unmarshal([]byte(raw), &env); err != nil {
			return nil, err
		}
		out = append(out, env)
	}
	return out, rows.Err()
}

func (s *Store) queryMessages(query string, args ...any) ([]LocalMessage, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LocalMessage
	for rows.Next() {
		var msg LocalMessage
		var recipients string
		var plaintext string
		if err := rows.Scan(&msg.MessageID, &msg.ConversationID, &msg.TaskID, &msg.Type, &msg.Sender, &recipients,
			&msg.Direction, &msg.DeliveryState, &msg.ProcessingState, &plaintext, &msg.CreatedAt, &msg.ReceivedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(recipients), &msg.Recipients)
		msg.Plaintext = json.RawMessage(plaintext)
		out = append(out, msg)
	}
	return out, rows.Err()
}

func (s *Store) UpsertTask(task TaskRecord) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if task.UpdatedAt == "" {
		task.UpdatedAt = now
	}
	_, err := s.db.Exec(
		`INSERT INTO tasks(task_id, conversation_id, creator, assignee, title, description, status, max_revisions, revision_count, created_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(task_id) DO UPDATE SET
		 	status=excluded.status,
		 	revision_count=excluded.revision_count,
		 	updated_at=excluded.updated_at`,
		task.TaskID, task.ConversationID, task.Creator, task.Assignee, task.Title, task.Description, task.Status,
		task.MaxRevisions, task.RevisionCount, now, task.UpdatedAt,
	)
	return err
}

func (s *Store) Task(taskID string) (TaskRecord, error) {
	var task TaskRecord
	err := s.db.QueryRow(
		`SELECT task_id, conversation_id, creator, assignee, title, description, status, max_revisions, revision_count, updated_at FROM tasks WHERE task_id = ?`,
		taskID,
	).Scan(&task.TaskID, &task.ConversationID, &task.Creator, &task.Assignee, &task.Title, &task.Description, &task.Status, &task.MaxRevisions, &task.RevisionCount, &task.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TaskRecord{}, ErrNotFound
		}
		return TaskRecord{}, err
	}
	return task, nil
}

func (s *Store) Tasks(limit int) ([]TaskRecord, error) {
	rows, err := s.db.Query(
		`SELECT task_id, conversation_id, creator, assignee, title, description, status, max_revisions, revision_count, updated_at FROM tasks ORDER BY updated_at DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TaskRecord
	for rows.Next() {
		var task TaskRecord
		if err := rows.Scan(&task.TaskID, &task.ConversationID, &task.Creator, &task.Assignee, &task.Title, &task.Description, &task.Status, &task.MaxRevisions, &task.RevisionCount, &task.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, rows.Err()
}

func (s *Store) Log(level string, eventType string, message string, data any) {
	raw, _ := json.Marshal(data)
	_, _ = s.db.Exec(
		`INSERT INTO logs(level, event_type, message, data_json, created_at) VALUES(?, ?, ?, ?, ?)`,
		level, eventType, message, string(raw), time.Now().UTC().Format(time.RFC3339Nano),
	)
}
