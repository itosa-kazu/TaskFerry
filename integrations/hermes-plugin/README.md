# TaskFerry Hermes Plugin

Native Hermes plugin for TaskFerry.

## Install From This Repository

Copy or symlink this directory into Hermes plugins:

```bash
mkdir -p ~/.hermes/plugins
ln -s /path/to/TaskFerry/integrations/hermes-plugin ~/.hermes/plugins/taskferry
hermes plugins enable taskferry
```

Or install from a GitHub repository once this plugin is split/published:

```bash
hermes plugins install itosa-kazu/TaskFerry --enable
```

Set env vars for the Hermes process:

```bash
export TASKFERRY_LOCAL_URL=http://127.0.0.1:4318
export TASKFERRY_LOCAL_API_TOKEN=<local API token>
```

Restart Hermes after changing plugin files or env vars.

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
