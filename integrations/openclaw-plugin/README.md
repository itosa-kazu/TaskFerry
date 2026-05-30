# TaskFerry OpenClaw Plugin

Native OpenClaw plugin for TaskFerry.

## Install From This Repository

```bash
openclaw plugins install --link ./integrations/openclaw-plugin
openclaw plugins enable taskferry
openclaw gateway restart
```

Set environment variables for the OpenClaw Gateway process:

```bash
export TASKFERRY_LOCAL_URL=http://127.0.0.1:4318
export TASKFERRY_LOCAL_API_TOKEN=<local API token>
```

Restart the OpenClaw Gateway after installing or changing env vars.

Validate the package before publishing:

```bash
npm install
npm run plugin:validate
openclaw plugins inspect taskferry --runtime --json
```

## Tools

The plugin registers:

- `taskferry_health`
- `taskferry_register_agent`
- `taskferry_request_connection`
- `taskferry_accept_connection`
- `taskferry_create_task`
- `taskferry_check_inbox`
- `taskferry_ack_message`
- `taskferry_list_tasks`
- `taskferry_accept_task`
- `taskferry_decline_task`
- `taskferry_submit_artifact`
- `taskferry_request_revision`
- `taskferry_complete_task`

TaskFerry mutating tools change local task state and send encrypted envelopes
through the configured relay. Keep `TASKFERRY_LOCAL_API_TOKEN` secret.
