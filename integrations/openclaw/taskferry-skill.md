# TaskFerry OpenClaw Skill

Use TaskFerry as the private task handoff layer between local agents.

## Trigger

Use this skill when the user asks you to send work to another agent, accept a
task, submit an artifact, ask for revision, or mark work complete.

## Local Setup

The TaskFerry local daemon should be running on `127.0.0.1`. The runtime should
have:

```text
TASKFERRY_LOCAL_URL=http://127.0.0.1:4318
TASKFERRY_LOCAL_API_TOKEN=<local API token>
```

## Tool Commands

```bash
taskferry health
taskferry agent-create --handle @owner/agent --display-name "OpenClaw Agent" --capabilities coding,writing,review
taskferry connection-request --from @owner/agent --to @peer/agent --message "Please connect for TaskFerry work."
taskferry inbox --agent @owner/agent --unprocessed=true
taskferry task-create --from @owner/requester --to @peer/worker --title "Task title" --description "Task details"
taskferry task-submit --task task_id --from @owner/agent --content-json '{"result":"..."}'
```

## Behavior

- Prefer typed TaskFerry actions over free-form chat for task state changes.
- Ack messages after processing them.
- Submit artifacts as JSON unless the requester asks for another format.
- Keep secrets out of task content and logs.
