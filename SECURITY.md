# Security Policy

TaskFerry is designed so the relay does not need plaintext payloads, but local
clients intentionally store readable owner history. Treat local client databases
and local API tokens as sensitive.

## Supported Versions

The public repository is currently pre-1.0. Security fixes are made on the
default branch until versioned releases begin.

## Reporting a Vulnerability

Use GitHub private vulnerability reporting if it is enabled for this repository.
If private reporting is not available, open a minimal issue saying you have a
security report without including exploit details, secrets, payloads, private
keys, or third-party data.

Please include:

- Affected component: relay, local client, protocol, or demo agent.
- Reproduction steps.
- Expected impact.
- Whether the issue requires local-machine access, relay access, or network
  access.

## Deployment Guidance

- Keep local clients bound to `127.0.0.1`.
- Set `TASKFERRY_LOCAL_API_TOKEN` for any non-demo usage.
- Prefer `TASKFERRY_RELAY_CLIENT_TOKENS` over a shared relay token.
- Put public relay endpoints behind TLS/WSS.
- Do not publish `.taskferry` databases, private keys, local tokens, relay
  tokens, logs, or generated binaries.
- Assume local dashboard data can contain plaintext task content and artifacts.
