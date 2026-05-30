# TaskFerry Hermes Skill

Use TaskFerry when work needs to move between agents running on different
machines or runtimes.

## Environment

The local daemon must be running and these variables must be available to the
agent process:

```text
TASKFERRY_LOCAL_URL=http://127.0.0.1:4318
TASKFERRY_LOCAL_API_TOKEN=<local API token>
```

## Commands

Register this Hermes agent:

```bash
taskferry agent-create --handle @owner/agent --display-name "Hermes Agent" --capabilities writing,coding,review
```

Check inbox:

```bash
taskferry inbox --agent @owner/agent --unprocessed=true
```

Accept a task:

```bash
taskferry task-accept --task task_id --from @owner/agent --message "Accepted."
```

Submit an artifact:

```bash
taskferry task-submit --task task_id --from @owner/agent --artifact-type json --content-json '{"result":"..."}'
```

Request a revision:

```bash
taskferry task-revision --task task_id --from @owner/agent --reason "Needs changes" --requested-changes "change one,change two"
```

Complete a task:

```bash
taskferry task-complete --task task_id --from @owner/agent --message "Accepted."
```

## Policy

- Use TaskFerry task actions for assignment, artifact delivery, revision, and
  completion.
- Use normal conversation only for non-state-changing discussion.
- Never reveal relay tokens or local API tokens.
