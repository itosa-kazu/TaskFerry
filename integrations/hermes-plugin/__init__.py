"""TaskFerry Hermes plugin."""

from pathlib import Path

from . import schemas, tools


_TOOL_SPECS = [
    ("taskferry_health", schemas.TASKFERRY_HEALTH, tools.taskferry_health),
    ("taskferry_register_agent", schemas.TASKFERRY_REGISTER_AGENT, tools.taskferry_register_agent),
    ("taskferry_request_connection", schemas.TASKFERRY_REQUEST_CONNECTION, tools.taskferry_request_connection),
    ("taskferry_accept_connection", schemas.TASKFERRY_ACCEPT_CONNECTION, tools.taskferry_accept_connection),
    ("taskferry_create_task", schemas.TASKFERRY_CREATE_TASK, tools.taskferry_create_task),
    ("taskferry_check_inbox", schemas.TASKFERRY_CHECK_INBOX, tools.taskferry_check_inbox),
    ("taskferry_ack_message", schemas.TASKFERRY_ACK_MESSAGE, tools.taskferry_ack_message),
    ("taskferry_list_tasks", schemas.TASKFERRY_LIST_TASKS, tools.taskferry_list_tasks),
    ("taskferry_accept_task", schemas.TASKFERRY_ACCEPT_TASK, tools.taskferry_accept_task),
    ("taskferry_decline_task", schemas.TASKFERRY_DECLINE_TASK, tools.taskferry_decline_task),
    ("taskferry_submit_artifact", schemas.TASKFERRY_SUBMIT_ARTIFACT, tools.taskferry_submit_artifact),
    ("taskferry_request_revision", schemas.TASKFERRY_REQUEST_REVISION, tools.taskferry_request_revision),
    ("taskferry_complete_task", schemas.TASKFERRY_COMPLETE_TASK, tools.taskferry_complete_task),
]


def register(ctx):
    """Register TaskFerry tools and bundled skill."""
    for name, schema, handler in _TOOL_SPECS:
        ctx.register_tool(
            name=name,
            toolset="taskferry",
            schema=schema,
            handler=handler,
            description=schema["description"],
        )

    skill_path = Path(__file__).parent / "skills" / "taskferry" / "SKILL.md"
    if skill_path.exists():
        ctx.register_skill("taskferry", skill_path)
