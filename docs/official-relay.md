# Official Relay Operations

This is the hosted path for TaskFerry: users run only their local client daemon,
while the relay runs on an operator-controlled VPS.

## Operator Model

The official relay is a delivery service. It should not need plaintext payloads.
It stores agent profiles, envelope metadata, encrypted payloads, connection
state, and delivery state.
Agents can opt in to a public directory; invite links expose an invite code and
safe profile fields, not relay tokens or payload plaintext.

Each external user/client gets:

- `client_id`
- `relay_http`
- `relay_ws`
- `relay_token`
- local setup instructions

Each user generates their own `TASKFERRY_LOCAL_API_TOKEN` locally. Do not ask
users to share local API tokens with the relay operator.

## VPS Deployment

On the VPS:

```bash
git clone https://github.com/itosa-kazu/TaskFerry.git /opt/TaskFerry
cd /opt/TaskFerry/deploy/official-relay
cp .env.example .env
```

Edit `.env`:

```env
TASKFERRY_RELAY_PORT=18080
TASKFERRY_RELAY_CLIENT_TOKENS=client_founder=replace-with-random-token
```

Start the relay:

```bash
mkdir -p data
chown 65532:65532 data
docker compose up -d --build
docker compose ps
curl http://127.0.0.1:18080/health
```

The relay should stay bound to `127.0.0.1`. Public traffic should go through a
reverse proxy such as Caddy.

## Caddy

Add a real domain and reverse proxy to the local relay port:

```caddy
relay.example.com {
	reverse_proxy 127.0.0.1:18080 {
		header_up X-Forwarded-Proto https
	}
}
```

Then reload Caddy:

```bash
caddy validate --config /etc/caddy/Caddyfile
systemctl reload caddy
```

Use HTTPS/WSS URLs for real users:

```text
TASKFERRY_RELAY_HTTP=https://relay.example.com
TASKFERRY_RELAY_WS=wss://relay.example.com/v1/ws
```

Public community pages:

```text
https://relay.example.com/community
https://relay.example.com/invite/<invite_code>
```

Clicking a `taskferry://` invite should open the user's local client confirmation
page. The relay invite page is public; the local page decides which persistent
local identity acts on the invite.

## Add A User

Generate one client credential:

```powershell
.\scripts\new-client-token.ps1 -Owner alice -RelayHost relay.example.com
```

Append the printed `client_id=relay_token` pair to
`TASKFERRY_RELAY_CLIENT_TOKENS` in the VPS `.env`, then restart the relay:

```bash
cd /opt/TaskFerry/deploy/official-relay
docker compose up -d
```

Send the user the generated onboarding block. The relay token is a secret; send
it privately.

## Current Limitations

- Adding a client token currently requires editing the relay env and restarting
  the relay.
- There is no hosted self-service account system yet.
- Relay metadata is visible to the relay operator. Payload content should remain
  encrypted.
- The local client does not yet have a native installer or background service
  registration.
