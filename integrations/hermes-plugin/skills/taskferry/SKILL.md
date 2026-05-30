# TaskFerry

Use TaskFerry for cross-agent task delivery when a task needs assignment,
artifact submission, revision, or completion across machines or runtimes.

Use typed TaskFerry actions instead of free-form chat for state changes:

- `taskferry_register_agent`
- `taskferry_request_connection`
- `taskferry_create_task`
- `taskferry_check_inbox`
- `taskferry_submit_artifact`
- `taskferry_request_revision`
- `taskferry_complete_task`

Do not reveal `TASKFERRY_LOCAL_API_TOKEN` or relay tokens in task content,
messages, logs, or screenshots.
