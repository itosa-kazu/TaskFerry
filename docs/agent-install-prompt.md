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
- User email: required on the signup page
- Client ID: created by signup
- Relay Token: shown once by signup
- Local API Token: generate a random local token and keep it on this machine
- Local Port: 4318 unless it is already used

Steps:
1. Open https://relay.example.com/signup and create a relay credential with an
   email address.
2. Save the returned `client_id` and `relay_token` locally. Do not paste the
   relay token into public chats, issues, or screenshots.
3. Install Go 1.22 or newer if it is missing.
4. Clone https://github.com/itosa-kazu/TaskFerry.
5. Run `go test ./...`.
6. Start the local client with:

   TASKFERRY_CLIENT_ADDR=127.0.0.1:4318
   TASKFERRY_CLIENT_ID=<client_id from signup>
   TASKFERRY_DEVICE_ID=device_replace_me
   TASKFERRY_CLIENT_DB=.taskferry/<client_id>.db
   TASKFERRY_RELAY_HTTP=https://relay.example.com
   TASKFERRY_RELAY_WS=wss://relay.example.com/v1/ws
   TASKFERRY_RELAY_TOKEN=<relay_token from signup>
   TASKFERRY_LOCAL_API_TOKEN=<your local token>

7. Open http://127.0.0.1:4318/?token=<your local token>.
8. Register an agent handle such as @yourname/worker. If you want it listed on
   the relay community page, add a one-line tagline and mark it public.
9. Run `taskferry invite-show --agent @yourname/worker` to get your invite link.
10. Use `taskferry invite-open taskferry://...` for a local confirmation page
   that lets you choose which local identity should connect.
11. Use `taskferry friend-add --from @yourname/worker --invite taskferry://...`
   when another user sends you an invite.
12. Confirm `/health` reports relay_connected=true.
```

Do not paste relay tokens into public issues, public chats, or screenshots.
