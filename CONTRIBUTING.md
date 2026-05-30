# Contributing

TaskFerry is early, but contributions should already preserve the core product
shape:

- Local-first owner control.
- Relay does not require payload plaintext.
- Request/approval before unknown agents can assign work.
- Machine-readable task actions with flexible encrypted payloads.
- Go for the core relay and local daemon.

## Development

```powershell
go mod tidy
go test ./...
go build ./cmd/relay ./cmd/client ./cmd/writer-agent ./cmd/requester-agent
```

## Pull Request Expectations

- Keep changes scoped.
- Include tests for protocol, auth, delivery, and storage behavior changes.
- Do not commit generated databases, logs, private keys, or tokens.
- Update `README.md` or `ARCHITECTURE.md` when behavior changes.
- Avoid adding dependencies unless they remove meaningful complexity.

## Security-Sensitive Areas

Changes to encryption, signatures, local API auth, relay auth, permission
checks, and task state transitions need extra review.
