package relay

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
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

type AgentInvite struct {
	Code      string
	Handle    string
	CreatedAt string
	UpdatedAt string
}

type ClientCredential struct {
	ClientID  string
	Token     string
	OwnerName string
	Email     string
	CreatedAt string
	UpdatedAt string
}

type StoreStats struct {
	Clients         int `json:"clients"`
	Agents          int `json:"agents"`
	PublicAgents    int `json:"public_agents"`
	Invites         int `json:"invites"`
	Connections     int `json:"connections"`
	QueuedMessages  int `json:"queued_messages"`
	PendingMessages int `json:"pending_messages"`
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
		`CREATE TABLE IF NOT EXISTS clients (
			client_id TEXT PRIMARY KEY,
			token TEXT NOT NULL,
			owner_name TEXT NOT NULL,
			email TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS client_emails (
			email TEXT PRIMARY KEY,
			client_id TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`INSERT OR IGNORE INTO client_emails(email, client_id, created_at)
		 SELECT lower(email), client_id, created_at
		 FROM clients
		 WHERE trim(email) <> ''
		 ORDER BY created_at`,
		`CREATE TABLE IF NOT EXISTS agent_invites (
			code TEXT PRIMARY KEY,
			handle TEXT NOT NULL UNIQUE,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
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

func (s *Store) CreateClient(ownerName string, email string) (ClientCredential, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i := 0; i < 4; i++ {
		clientID := protocol.NewID("client")
		token := protocol.NewID("relay")
		tx, err := s.db.Begin()
		if err != nil {
			return ClientCredential{}, err
		}
		if email != "" {
			var existing string
			err = tx.QueryRow(`SELECT client_id FROM client_emails WHERE email = ?`, email).Scan(&existing)
			if err == nil {
				_ = tx.Rollback()
				return ClientCredential{}, ErrEmailExists
			}
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				_ = tx.Rollback()
				return ClientCredential{}, err
			}
			if _, err := tx.Exec(
				`INSERT INTO client_emails(email, client_id, created_at) VALUES(?, ?, ?)`,
				email, clientID, now,
			); err != nil {
				_ = tx.Rollback()
				return ClientCredential{}, ErrEmailExists
			}
		}
		_, err = tx.Exec(
			`INSERT INTO clients(client_id, token, owner_name, email, created_at, updated_at)
			 VALUES(?, ?, ?, ?, ?, ?)`,
			clientID, token, ownerName, email, now, now,
		)
		if err == nil {
			if err := tx.Commit(); err != nil {
				return ClientCredential{}, err
			}
			return ClientCredential{ClientID: clientID, Token: token, OwnerName: ownerName, Email: email, CreatedAt: now, UpdatedAt: now}, nil
		}
		_ = tx.Rollback()
	}
	return ClientCredential{}, errors.New("client_create_failed")
}

func (s *Store) ClientToken(clientID string) (string, error) {
	var token string
	err := s.db.QueryRow(`SELECT token FROM clients WHERE client_id = ?`, clientID).Scan(&token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return token, nil
}

func (s *Store) ClientCount() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM clients`).Scan(&count)
	return count, err
}

func (s *Store) Stats() (StoreStats, error) {
	var stats StoreStats
	queries := []struct {
		target *int
		sql    string
	}{
		{&stats.Clients, `SELECT COUNT(*) FROM clients`},
		{&stats.Agents, `SELECT COUNT(*) FROM agents`},
		{&stats.PublicAgents, `SELECT COUNT(*) FROM agents WHERE profile_json LIKE '%"public_profile":true%'`},
		{&stats.Invites, `SELECT COUNT(*) FROM agent_invites`},
		{&stats.Connections, `SELECT COUNT(*) FROM connections`},
		{&stats.QueuedMessages, `SELECT COUNT(*) FROM relay_messages`},
		{&stats.PendingMessages, `SELECT COUNT(*) FROM relay_messages WHERE delivery_state = 'pending'`},
	}
	for _, query := range queries {
		if err := s.db.QueryRow(query.sql).Scan(query.target); err != nil {
			return StoreStats{}, err
		}
	}
	return stats, nil
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
	if err != nil {
		return err
	}
	_, err = s.EnsureInvite(agent.Handle)
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

func (s *Store) EnsureInvite(handle string) (AgentInvite, error) {
	if invite, err := s.InviteByHandle(handle); err == nil {
		return invite, nil
	} else if !errors.Is(err, ErrNotFound) {
		return AgentInvite{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i := 0; i < 4; i++ {
		code := protocol.NewID("inv")
		_, err := s.db.Exec(
			`INSERT INTO agent_invites(code, handle, created_at, updated_at) VALUES(?, ?, ?, ?)`,
			code, handle, now, now,
		)
		if err == nil {
			return AgentInvite{Code: code, Handle: handle, CreatedAt: now, UpdatedAt: now}, nil
		}
	}
	return AgentInvite{}, errors.New("invite_create_failed")
}

func (s *Store) InviteByHandle(handle string) (AgentInvite, error) {
	var invite AgentInvite
	err := s.db.QueryRow(
		`SELECT code, handle, created_at, updated_at FROM agent_invites WHERE handle = ?`,
		handle,
	).Scan(&invite.Code, &invite.Handle, &invite.CreatedAt, &invite.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AgentInvite{}, ErrNotFound
		}
		return AgentInvite{}, err
	}
	return invite, nil
}

func (s *Store) Invite(code string) (AgentInvite, error) {
	var invite AgentInvite
	err := s.db.QueryRow(
		`SELECT code, handle, created_at, updated_at FROM agent_invites WHERE code = ?`,
		code,
	).Scan(&invite.Code, &invite.Handle, &invite.CreatedAt, &invite.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AgentInvite{}, ErrNotFound
		}
		return AgentInvite{}, err
	}
	return invite, nil
}

func (s *Store) PublicAgents(limit int, relayHTTP string) ([]protocol.DirectoryAgent, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT profile_json FROM agents ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	var profiles []protocol.AgentProfile
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			_ = rows.Close()
			return nil, err
		}
		var profile protocol.AgentProfile
		if err := json.Unmarshal([]byte(raw), &profile); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if !profile.PublicProfile {
			continue
		}
		profiles = append(profiles, profile)
		if len(profiles) >= limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	var out []protocol.DirectoryAgent
	for _, profile := range profiles {
		invite, err := s.EnsureInvite(profile.Handle)
		if err != nil {
			return nil, err
		}
		out = append(out, DirectoryAgent(profile, invite.Code, relayHTTP))
	}
	return out, nil
}

func DirectoryAgent(profile protocol.AgentProfile, inviteCode string, relayHTTP string) protocol.DirectoryAgent {
	relayHTTP = strings.TrimRight(relayHTTP, "/")
	host := strings.TrimPrefix(strings.TrimPrefix(relayHTTP, "https://"), "http://")
	tagline := profile.Tagline
	if tagline == "" {
		tagline = profile.Description
	}
	return protocol.DirectoryAgent{
		Handle:        profile.Handle,
		DisplayName:   profile.DisplayName,
		Tagline:       tagline,
		Description:   profile.Description,
		Capabilities:  profile.Capabilities,
		InviteCode:    inviteCode,
		InviteURL:     "taskferry://" + host + "/invite/" + inviteCode,
		WebInviteURL:  relayHTTP + "/invite/" + inviteCode,
		PublicProfile: profile.PublicProfile,
		UpdatedAt:     profile.UpdatedAt,
	}
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
