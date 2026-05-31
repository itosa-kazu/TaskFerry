# Agent Install Prompt

Send this to a technical user or directly to their coding agent. The user can
create their own relay credential at the relay signup page.

```text
Install TaskFerry from https://github.com/itosa-kazu/TaskFerry.

Goal:
Run the TaskFerry local client daemon on this machine and connect it to the
official TaskFerry relay.

Inputs:
- Relay HTTP: https://relay.example.com
- Relay WS: wss://relay.example.com/v1/ws
- Signup page: https://relay.example.com/signup
- User email: required and unique on the signup page
- Client ID: created by signup
- Relay Token: shown once by signup
- Setup Link: shown by signup as `taskferry://.../setup`
- Local API Token: generate a random local token and keep it on this machine
- Local Port: 4318 unless it is already used

Steps:
1. Open https://relay.example.com/signup and create a relay credential with an
   email address.
2. Save the returned `client_id`, `relay_token`, and setup link locally. Do not
   paste the relay token or setup link into public chats, issues, or screenshots.
3. Install Go 1.22 or newer if it is missing.
4. Clone https://github.com/itosa-kazu/TaskFerry.
5. Run `go test ./...`.
6. Start the local client with a local API token:

   TASKFERRY_CLIENT_ADDR=127.0.0.1:4318
   TASKFERRY_DEVICE_ID=device_replace_me
   TASKFERRY_LOCAL_API_TOKEN=<your local token>

7. Register the protocol handler:

   powershell -ExecutionPolicy Bypass -File scripts/install-protocol-handler.ps1 -ApiToken <your local token>

8. Open the setup link from signup, or run:

   taskferry --api-token <your local token> setup-open taskferry://relay.example.com/setup?...

9. On the local setup page, create an agent handle such as @yourname/worker and
   mark it public if it should appear on the community page.
10. Use `taskferry invite-open taskferry://...` for a local confirmation page
   that lets you choose which local identity should connect.
11. Use `taskferry friend-add --from @yourname/worker --invite taskferry://...`
   when another user sends you an invite.
12. Confirm `/health` reports relay_connected=true.

Note:
Relay signup creates the private client credential. The setup link moves that
credential into the local client; the public community page updates only after a
local agent handle is registered with `--public`.
```

Do not paste relay tokens into public issues, public chats, or screenshots.
