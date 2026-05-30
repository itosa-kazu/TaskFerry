# TaskFerry

TaskFerry is a private, self-hostable task relay for local AI agents.

It is not another agent social network or a human-style chat app. TaskFerry is
for moving work between agents that run on different machines while keeping the
owner in control:

- Local agents talk to a local Go daemon over `127.0.0.1`.
- The local daemon keeps readable owner history in SQLite.
- Payloads are encrypted before leaving the local machine.
- The relay routes metadata and encrypted payloads; it does not need plaintext.
- Unknown agents must request and receive approval before sending work.
- Task actions are typed: request, accept, submit artifact, request revision,
  resubmit, complete, or cancel.

## Why TaskFerry

Most agent messaging products start from the "WhatsApp for agents" metaphor.
TaskFerry starts from the work handoff problem:

```text
Agent A has a task.
Agent B runs somewhere else and can do it.
The handoff needs identity, delivery, artifact versions, revision requests,
approval gates, and an audit trail.
```

The ferry metaphor is deliberate: a local client loads a sealed work packet, the
relay carries it across the network, and only the recipient local client opens
it.

## Architecture

```text
Local Agent
  -> localhost API
  -> TaskFerry Local Client Daemon
  -> encrypted envelope over WebSocket/HTTPS
  -> TaskFerry Relay
  -> encrypted envelope over WebSocket
  -> Remote Local Client Daemon
  -> localhost inbox
  -> Remote Local Agent
```

Core implementation:

- Go relay/gateway.
- Go local client daemon.
- Local web dashboard exposed by the daemon.
- SQLite for local owner history.
- SQLite for the current single-node relay store.
- X25519 + AES-GCM for encrypted payloads.
- Ed25519 for envelope signatures.

See [ARCHITECTURE.md](./ARCHITECTURE.md) for the full engineering design.

## Current Status

This repository contains the first production core:

- Relay registration and WebSocket delivery.
- Local client daemon and dashboard.
- Agent key generation.
- Encrypted outbound payloads.
- Decrypted local owner history.
- Connection request/accept flow.
- Permission checks at relay.
- Task request, artifact submit, revision request, and completion flow.
- Rule-based demo agents.
- Local API bearer token support.
- Per-client relay token mapping support.

Known production gaps before public hosted use:

- TLS/WSS reverse proxy configuration for hosted relay deployments.
- Installer/release packaging.
- Owner UI for editing permissions.
- Artifact object storage.
- Multi-recipient encryption.

## Build

Install Go 1.22+.

```powershell
go mod tidy
go test ./...
go build ./cmd/relay ./cmd/client ./cmd/writer-agent ./cmd/requester-agent
```

## Run Locally

Open one terminal per process.

Run the relay:

```powershell
$env:TASKFERRY_RELAY_ADDR="127.0.0.1:8080"
$env:TASKFERRY_RELAY_DB=".taskferry\relay.db"
$env:TASKFERRY_RELAY_CLIENT_TOKENS="client_alice=alice-relay-token,client_bob=bob-relay-token"
go run ./cmd/relay
```

Run Alice's local client:

```powershell
$env:TASKFERRY_CLIENT_ADDR="127.0.0.1:4318"
$env:TASKFERRY_CLIENT_ID="client_alice"
$env:TASKFERRY_DEVICE_ID="device_alice"
$env:TASKFERRY_CLIENT_DB=".taskferry\alice.db"
$env:TASKFERRY_RELAY_HTTP="http://127.0.0.1:8080"
$env:TASKFERRY_RELAY_WS="ws://127.0.0.1:8080/v1/ws"
$env:TASKFERRY_RELAY_TOKEN="alice-relay-token"
$env:TASKFERRY_LOCAL_API_TOKEN="alice-local-token"
go run ./cmd/client
```

Run Bob's local client:

```powershell
$env:TASKFERRY_CLIENT_ADDR="127.0.0.1:4319"
$env:TASKFERRY_CLIENT_ID="client_bob"
$env:TASKFERRY_DEVICE_ID="device_bob"
$env:TASKFERRY_CLIENT_DB=".taskferry\bob.db"
$env:TASKFERRY_RELAY_HTTP="http://127.0.0.1:8080"
$env:TASKFERRY_RELAY_WS="ws://127.0.0.1:8080/v1/ws"
$env:TASKFERRY_RELAY_TOKEN="bob-relay-token"
$env:TASKFERRY_LOCAL_API_TOKEN="bob-local-token"
go run ./cmd/client
```

Dashboards:

- Alice: <http://127.0.0.1:4318/?token=alice-local-token>
- Bob: <http://127.0.0.1:4319/?token=bob-local-token>

## Run The Demo Agents

Start the writer first:

```powershell
go run ./cmd/writer-agent --base-url http://127.0.0.1:4319 --api-token bob-local-token
```

Then start the requester:

```powershell
go run ./cmd/requester-agent --base-url http://127.0.0.1:4318 --api-token alice-local-token
```

The demo flow:

```text
@alice/requester requests a connection to @bob/writer
@bob/writer accepts
@alice/requester creates a task
@bob/writer accepts the task
@bob/writer submits artifact version 1
@alice/requester requests a revision
@bob/writer submits artifact version 2
@alice/requester completes the task
```

Both dashboards should show the task as `completed`.

## Environment Variables

Preferred names:

```text
TASKFERRY_RELAY_ADDR
TASKFERRY_RELAY_DB
TASKFERRY_RELAY_TOKEN
TASKFERRY_RELAY_CLIENT_TOKENS
TASKFERRY_CLIENT_ADDR
TASKFERRY_CLIENT_ID
TASKFERRY_DEVICE_ID
TASKFERRY_OWNER_ID
TASKFERRY_CLIENT_DB
TASKFERRY_RELAY_HTTP
TASKFERRY_RELAY_WS
TASKFERRY_LOCAL_API_TOKEN
```

`TASKFERRY_RELAY_CLIENT_TOKENS` accepts comma-separated `client_id=token`
pairs, for example:

```text
client_alice=alice-relay-token,client_bob=bob-relay-token
```

The previous `AGENTCHAT_*` names are still accepted as legacy aliases while the
codebase is being renamed.

## Security Notes

- Keep the local client daemon bound to loopback unless you add your own
  authentication and network controls.
- Set `TASKFERRY_LOCAL_API_TOKEN` before connecting non-demo agents.
- Prefer `TASKFERRY_RELAY_CLIENT_TOKENS` over one shared relay token.
- Put public relay deployments behind TLS/WSS.
- Do not commit `.taskferry` databases, private keys, local tokens, logs, or
  generated binaries.

See [SECURITY.md](./SECURITY.md) for reporting and deployment guidance.

## License

Apache-2.0. See [LICENSE](./LICENSE).
