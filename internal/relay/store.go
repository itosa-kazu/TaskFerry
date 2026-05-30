package relay

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/itosa-kazu/TaskFerry/internal/protocol"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type QueuedMessage struct {
	MessageID string
	Recipient string
	Envelope  protocol.Envelope
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
			client_id TEXT NOT NULL,
			profile_json TEXT NOT NULL,
			signing_public_key TEXT NOT NULL,
			encryption_public_key TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_agents_client ON agents(client_id)`,
		`CREATE TABLE IF NOT EXISTS connections (
			from_handle TEXT NOT NULL,
			to_handle TEXT NOT NULL,
			status TEXT NOT NULL,
			permissions_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(from_handle, to_handle)
		)`,
		`CREATE TABLE IF NOT EXISTS relay_messages (
			message_id TEXT NOT NULL,
			recipient TEXT NOT NULL,
			envelope_json TEXT NOT NULL,
			delivery_state TEXT NOT NULL,
			created_at TEXT NOT NULL,
			delivered_at TEXT,
			PRIMARY KEY(message_id, recipient)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_relay_messages_recipient_state ON relay_messages(recipient, delivery_state, created_at)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) UpsertAgent(clientID string, deviceID string, agent protocol.AgentProfile) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	profileJSON, err := json.Marshal(agent)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO agents(handle, agent_id, owner_id, device_id, client_id, profile_json, signing_public_key, encryption_public_key, created_at, updated_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(handle) DO UPDATE SET
		 	agent_id=excluded.agent_id,
		 	owner_id=excluded.owner_id,
		 	device_id=excluded.device_id,
		 	client_id=excluded.client_id,
		 	profile_json=excluded.profile_json,
		 	signing_public_key=excluded.signing_public_key,
		 	encryption_public_key=excluded.encryption_public_key,
		 	updated_at=excluded.updated_at`,
		agent.Handle, agent.AgentID, agent.OwnerID, deviceID, clientID, string(profileJSON),
		agent.SigningPublicKey, agent.EncryptionPublicKey, now, now,
	)
	return err
}

func (s *Store) Agent(handle string) (protocol.AgentProfile, string, error) {
	var profileJSON string
	var clientID string
	err := s.db.QueryRow(`SELECT profile_json, client_id FROM agents WHERE handle = ?`, handle).Scan(&profileJSON, &clientID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return protocol.AgentProfile{}, "", ErrNotFound
		}
		return protocol.AgentProfile{}, "", err
	}
	var profile protocol.AgentProfile
	if err := json.Unmarshal([]byte(profileJSON), &profile); err != nil {
		return protocol.AgentProfile{}, "", err
	}
	return profile, clientID, nil
}

func (s *Store) ClientAgents(clientID string) ([]protocol.AgentProfile, error) {
	rows, err := s.db.Query(`SELECT profile_json FROM agents WHERE client_id = ? ORDER BY handle`, clientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []protocol.AgentProfile
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var profile protocol.AgentProfile
		if err := json.Unmarshal([]byte(raw), &profile); err != nil {
			return nil, err
		}
		out = append(out, profile)
	}
	return out, rows.Err()
}

func (s *Store) ApproveConnection(a string, b string, permissions protocol.PermissionSet) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	raw, err := json.Marshal(permissions)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, pair := range [][2]string{{a, b}, {b, a}} {
		if _, err := tx.Exec(
			`INSERT INTO connections(from_handle, to_handle, status, permissions_json, created_at, updated_at)
			 VALUES(?, ?, 'approved', ?, ?, ?)
			 ON CONFLICT(from_handle, to_handle) DO UPDATE SET
			 	status='approved',
			 	permissions_json=excluded.permissions_json,
			 	updated_at=excluded.updated_at`,
			pair[0], pair[1], string(raw), now, now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ConnectionPermissions(from string, to string) (protocol.PermissionSet, bool, error) {
	var status string
	var raw string
	err := s.db.QueryRow(
		`SELECT status, permissions_json FROM connections WHERE from_handle = ? AND to_handle = ?`,
		from, to,
	).Scan(&status, &raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return protocol.PermissionSet{}, false, nil
		}
		return protocol.PermissionSet{}, false, err
	}
	if status != "approved" {
		return protocol.PermissionSet{}, false, nil
	}
	var p protocol.PermissionSet
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return protocol.PermissionSet{}, false, err
	}
	return p, true, nil
}

func (s *Store) StoreMessage(env protocol.Envelope, recipient string, state protocol.DeliveryState) error {
	raw, err := json.Marshal(env)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO relay_messages(message_id, recipient, envelope_json, delivery_state, created_at)
		 VALUES(?, ?, ?, ?, ?)
		 ON CONFLICT(message_id, recipient) DO UPDATE SET
		 	envelope_json=excluded.envelope_json,
		 	delivery_state=excluded.delivery_state`,
		env.ID, recipient, string(raw), state, env.CreatedAt,
	)
	return err
}

func (s *Store) MarkDelivered(messageID string, recipient string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(
		`UPDATE relay_messages SET delivery_state = ?, delivered_at = ? WHERE message_id = ? AND recipient = ?`,
		protocol.DeliveryDeliveredToClient, now, messageID, recipient,
	)
	return err
}

func (s *Store) PendingForRecipient(recipient string, limit int) ([]QueuedMessage, error) {
	rows, err := s.db.Query(
		`SELECT message_id, recipient, envelope_json
		 FROM relay_messages
		 WHERE recipient = ? AND delivery_state != ?
		 ORDER BY created_at
		 LIMIT ?`,
		recipient, protocol.DeliveryDeliveredToClient, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QueuedMessage
	for rows.Next() {
		var msg QueuedMessage
		var raw string
		if err := rows.Scan(&msg.MessageID, &msg.Recipient, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &msg.Envelope); err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, rows.Err()
}
