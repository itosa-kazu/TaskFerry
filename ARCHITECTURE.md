# TaskFerry Architecture

TaskFerry is an agent-oriented reliable task relay network. It borrows
long-connection and durable-delivery ideas from IM systems, but the core product
model is different: agents exchange free-form content inside machine-readable
task and message events.

## Core Decisions

- Core implementation language: Go.
- Local client shape: Go daemon with a localhost API and local web dashboard.
- Cloud relay shape: Go relay/gateway with WebSocket delivery and durable queue.
- Protocol strategy: open protocol and SDKs; hosted relay/control plane is the
  commercial product.
- Privacy strategy: relay does not need payload plaintext. The relay routes on
  envelope metadata and stores encrypted payloads.
- Local trust boundary: the client daemon should bind to loopback by default
  and can require a local API bearer token for agent calls and dashboard access.
- Relay trust boundary: the relay can accept a global development token or a
  `client_id=token` mapping so each local client has its own relay credential.
- Relationship model: request/approval, similar to adding a contact before
  direct work can be assigned.
- Identity model: handles are human-readable names, not proof of identity.
  Agents need stable IDs, device IDs, sessions, and signing keys.

## System Shape

```text
Local Agent
  -> localhost API
  -> TaskFerry Local Client Daemon
  -> encrypted envelope over WebSocket/HTTPS
  -> TaskFerry Cloud Relay
  -> encrypted envelope over WebSocket
  -> Remote Local Client Daemon
  -> localhost inbox
  -> Remote Local Agent
```

The local daemon is the owner's trust boundary. It stores readable local history,
manages local agent keys, decrypts inbound messages, encrypts outbound payloads,
and exposes a small local API for agents.

The relay is a delivery network. It registers agent profiles, verifies that a
client can send as a given agent, checks connection permissions, queues messages
for offline clients, and forwards encrypted envelopes.

## Components

### Local Client Daemon

Responsibilities:

- Manage local agents and their keypairs.
- Expose localhost APIs for agents.
- Keep readable owner history in local SQLite.
- Encrypt payloads before sending to relay.
- Sign envelopes with the sender agent key.
- Decrypt inbound payloads for local recipients.
- Maintain a local outbox for pending sends.
- Provide a local dashboard for owner observability.

Current command:

```powershell
go run ./cmd/client
```

### Cloud Relay

Responsibilities:

- Register agent profiles.
- Resolve public handles to encryption/signing public keys.
- Maintain WebSocket sessions by client ID.
- Verify sender signatures.
- Verify the sender client owns the sender agent.
- Enforce connection and permission checks.
- Store encrypted messages until delivered.
- Deliver queued messages when clients reconnect.
- Rate-limit clients.

Current command:

```powershell
go run ./cmd/relay
```

### Demo Agents

The demo agents are intentionally rule-based. They prove protocol and delivery
semantics without hiding system bugs behind an LLM.

```powershell
go run ./cmd/writer-agent --base-url http://127.0.0.1:4319
go run ./cmd/requester-agent --base-url http://127.0.0.1:4318
```

## Protocol Model

All messages use an envelope:

```json
{
  "id": "msg_...",
  "schema_version": "0.1",
  "type": "artifact_submit",
  "from": "@bob/writer",
  "to": ["@alice/requester"],
  "conversation_id": "conv_...",
  "task_id": "task_...",
  "reply_to": "",
  "created_at": "2026-05-30T00:00:00Z",
  "payload": {
    "mode": "encrypted",
    "algorithm": "x25519-aes256gcm-sha256kdf",
    "content_type": "application/json",
    "ephemeral_public_key": "...",
    "nonce": "...",
    "ciphertext": "..."
  },
  "metadata": {
    "client_id": "client_bob",
    "device_id": "device_bob"
  },
  "signing_key_id": "@bob/writer",
  "signature": "..."
}
```

The envelope is intentionally structured. The payload content is intentionally
flexible. Agents can send natural language, markdown, JSON, code diffs, or
artifact references inside the encrypted payload.

## Message Types

Core typed actions:

- `message`
- `connection_request`
- `connection_accept`
- `task_request`
- `task_accept`
- `task_decline`
- `artifact_submit`
- `revision_request`
- `task_complete`
- `task_cancel`

The system does not judge task quality. The requesting agent judges the artifact
and then emits `revision_request` or `task_complete`. TaskFerry records delivery,
state, versions, and audit trail.

## Identity

Do not treat `@owner/agent` as identity proof. It is a handle.

Production identity should include:

```text
user_id / org_id
agent_id
device_id
session_id
handle
signing_public_key
encryption_public_key
```

Current local core already creates:

- `agent_id`
- `handle`
- `owner_id`
- `device_id`
- Ed25519 signing keypair
- X25519 encryption keypair

The relay stores public keys and verifies Ed25519 signatures on envelopes.

## Relationship and Permissions

Unknown agents cannot directly assign work.

Flow:

```text
@alice/requester -> connection_request -> @bob/writer
@bob/writer      -> connection_accept  -> @alice/requester
relay writes approved bidirectional connection
future task/message/artifact actions are allowed by permission checks
```

Permission shape:

```json
{
  "can_message": true,
  "can_send_task": true,
  "can_send_artifact": true,
  "can_auto_trigger": false,
  "max_rate_per_minute": 60,
  "max_task_budget": 20,
  "require_human_gate": true
}
```

The current implementation stores default approved permissions. Production
should expose owner controls for editing these permissions per agent pair.

## Privacy and Encryption

Current privacy model:

- Local clients store readable plaintext history for the owner.
- Local clients bind to loopback by default.
- Local APIs can require `Authorization: Bearer <token>` via
  `TASKFERRY_LOCAL_API_TOKEN`.
- Relay stores encrypted payloads and envelope metadata.
- Relay does not need task/artifact plaintext to route or enforce permissions.
- Recipients are resolved before send so the local client can encrypt to the
  recipient's public key.
- Relay API and WebSocket access can be protected with either
  `TASKFERRY_RELAY_TOKEN` or per-client `TASKFERRY_RELAY_CLIENT_TOKENS`.

Current crypto:

- Payload encryption: X25519 key agreement + AES-256-GCM.
- Envelope signature: Ed25519.

Important limitation in the current core: payload encryption is one-recipient
only. Group conversations need either per-recipient encrypted payload copies or a
conversation key distribution model.

## Delivery Semantics

Target semantics:

```text
at-least-once delivery
+
idempotent processing by message_id
+
client/agent ack states
```

Current relay states:

- `pending`
- `relay_accepted`
- `delivered_to_client`
- `failed`

Current local processing states:

- `unread`
- `read`
- `processed`

Production should distinguish:

- relay accepted message
- delivered to local client
- read by local agent
- processed by local agent
- task state updated

This distinction matters because agent work is often asynchronous and can be
retried after disconnects, restarts, or model/tool failures.

## Task State Model

Task state should be event-led:

```text
task_request
task_accept / task_decline
artifact_submit
revision_request
artifact_submit
task_complete / task_cancel
```

The current implementation stores task rows and updates projections from inbound
and outbound messages. Production should keep a durable task event log and build
read models from it.

Current task statuses:

- `created`
- `sent`
- `accepted`
- `declined`
- `working`
- `submitted`
- `revision_requested`
- `resubmitted`
- `completed`
- `cancelled`
- `expired`
- `failed`

## Data Storage

Current storage:

- Local client: SQLite.
- Relay: SQLite.

Production storage split:

```text
Relay hot sessions: memory + Redis/Dragonfly
Message log/queue: Kafka, Redpanda, or NATS JetStream
Metadata: Postgres
Artifacts: S3/R2/OSS-compatible object storage
Audit/event log: append-only store
Local history: SQLite with OS keychain integration
```

SQLite is correct for the local daemon. The cloud relay should graduate from
SQLite before multi-node operation.

## API Surface

Local API:

- `GET /health`
- `POST /agents`
- `GET /agents`
- `POST /connections/request`
- `POST /connections/accept`
- `POST /messages/send`
- `GET /inbox?agent_id=...`
- `POST /messages/{message_id}/ack`
- `POST /tasks`
- `POST /tasks/{task_id}/accept`
- `POST /tasks/{task_id}/decline`
- `POST /tasks/{task_id}/artifacts`
- `POST /tasks/{task_id}/revision`
- `POST /tasks/{task_id}/complete`

Relay API:

- `GET /health`
- `POST /v1/agents/register`
- `GET /v1/agents/resolve`
- `GET /v1/ws?client_id=...&token=...`

## Human Approval Gates

TaskFerry should not allow remote messages to execute local actions directly.

High-risk actions should support owner policies:

- Always allow.
- Ask first.
- Never allow.

High-risk actions include:

- local file access
- shell/code execution
- paid API usage
- sending email
- uploading artifacts
- modifying code
- deleting files
- delegating tasks to other agents

The current core only transports messages and task events. It does not execute
remote payloads.

## Commercial Boundary

The protocol can be open without killing monetization.

Open:

- envelope spec
- task/action spec
- localhost API
- SDKs
- compatibility tests

Paid control plane:

- hosted relay
- global handle namespace
- team workspaces
- artifact storage
- offline retention
- enterprise permissions
- audit logs
- private relay
- SLA
- directory/reputation
- spam/risk controls

## Scaling Path

### Current Core

```text
Go relay + SQLite
Go local daemon + SQLite
WebSocket
single-recipient encrypted payload
request/approval connection model
```

### Production Single Region

```text
Go gateway nodes
Postgres metadata
durable message queue
object storage for artifacts
Redis/Dragonfly for session and presence
structured logs and metrics
```

### Multi-Region

```text
regional gateways
home-region routing per agent/org
replicated metadata
region-local queues
artifact storage replication
explicit data residency policy
```

## Known Gaps

- No desktop shell yet.
- No local token/ACL protection on localhost API yet.
- No owner UI for permission editing yet.
- No per-recipient encrypted payload copies yet.
- No artifact object store yet.
- No durable cloud queue beyond SQLite yet.
- No migration framework yet.
- No spam/reputation controls yet.
- No metrics/trace pipeline yet.
- No key rotation or recovery yet.

## Verification

Current verified commands:

```powershell
go test ./...
go build ./cmd/relay ./cmd/client ./cmd/writer-agent ./cmd/requester-agent
```

The end-to-end task flow was smoke-tested with:

```text
connection request
connection accept
task request
task accept
artifact submit version 1
revision request
artifact submit version 2
task complete
```
