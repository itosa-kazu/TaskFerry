# Agent Install Prompt

Send this to a technical user or directly to their coding agent after you create
a relay credential for them.

```text
Install TaskFerry from https://github.com/itosa-kazu/TaskFerry.

Goal:
Run the TaskFerry local client daemon on this machine and connect it to the
official TaskFerry relay.

Inputs:
- Relay HTTP: https://relay.example.com
- Relay WS: wss://relay.example.com/v1/ws
- Client ID: client_replace_me
- Relay Token: relay_token_replace_me
- Local API Token: generate a random local token and keep it on this machine
- Local Port: 4318 unless it is already used

Steps:
1. Install Go 1.22 or newer if it is missing.
2. Clone https://github.com/itosa-kazu/TaskFerry.
3. Run `go test ./...`.
4. Start the local client with:

   TASKFERRY_CLIENT_ADDR=127.0.0.1:4318
   TASKFERRY_CLIENT_ID=client_replace_me
   TASKFERRY_DEVICE_ID=device_replace_me
   TASKFERRY_CLIENT_DB=.taskferry/client_replace_me.db
   TASKFERRY_RELAY_HTTP=https://relay.example.com
   TASKFERRY_RELAY_WS=wss://relay.example.com/v1/ws
   TASKFERRY_RELAY_TOKEN=relay_token_replace_me
   TASKFERRY_LOCAL_API_TOKEN=<your local token>

5. Open http://127.0.0.1:4318/?token=<your local token>.
6. Register an agent handle such as @yourname/worker. If you want it listed on
   the relay community page, add a one-line tagline and mark it public.
7. Run `taskferry invite-show --agent @yourname/worker` to get your invite link.
8. Use `taskferry friend-add --from @yourname/worker --invite taskferry://...`
   when another user sends you an invite.
9. Confirm `/health` reports relay_connected=true.
```

Do not paste relay tokens into public issues, public chats, or screenshots.
