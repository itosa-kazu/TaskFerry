"""TaskFerry Hermes tool schemas."""


def obj(properties=None, required=None):
    return {
        "type": "object",
        "properties": properties or {},
        "required": required or [],
        "additionalProperties": False,
    }


def string(description=""):
    return {"type": "string", "description": description}


def array(description=""):
    return {
        "type": "array",
        "description": description,
        "items": {"type": "string"},
    }


def boolean(description=""):
    return {"type": "boolean", "description": description}


def integer(description=""):
    return {"type": "integer", "description": description}


TASKFERRY_HEALTH = {
    "name": "taskferry_health",
    "description": "Check local TaskFerry client health and relay connection state.",
    "parameters": obj(),
}

TASKFERRY_REGISTER_AGENT = {
    "name": "taskferry_register_agent",
    "description": "Register or update a local TaskFerry agent handle.",
    "parameters": obj(
        {
            "handle": string("Agent handle, e.g. @alice/worker."),
            "display_name": string("Human-readable display name."),
            "description": string("Agent description."),
            "capabilities": array("Capability tags."),
        },
        ["handle"],
    ),
}

TASKFERRY_REQUEST_CONNECTION = {
    "name": "taskferry_request_connection",
    "description": "Request approval to communicate with another TaskFerry agent.",
    "parameters": obj(
        {
            "from": string("Sender handle."),
            "to": string("Recipient handle."),
            "message": string("Request message."),
        },
        ["from", "to"],
    ),
}

TASKFERRY_ACCEPT_CONNECTION = {
    "name": "taskferry_accept_connection",
    "description": "Accept a TaskFerry connection request.",
    "parameters": obj(
        {"from": string("Accepting agent handle."), "to": string("Requester handle.")},
        ["from", "to"],
    ),
}

TASKFERRY_CREATE_TASK = {
    "name": "taskferry_create_task",
    "description": "Create a typed TaskFerry task for another agent.",
    "parameters": obj(
        {
            "from": string("Requester handle."),
            "to": string("Assignee handle."),
            "title": string("Task title."),
            "description": string("Task details."),
            "requirements": array("Acceptance requirements."),
            "max_revisions": integer("Maximum revision count."),
            "expected_format": string("Expected artifact format."),
        },
        ["from", "to", "title", "description"],
    ),
}

TASKFERRY_CHECK_INBOX = {
    "name": "taskferry_check_inbox",
    "description": "Read a local TaskFerry agent inbox.",
    "parameters": obj(
        {"agent": string("Agent handle."), "unprocessed": boolean("Only unprocessed messages.")},
        ["agent"],
    ),
}

TASKFERRY_ACK_MESSAGE = {
    "name": "taskferry_ack_message",
    "description": "Mark a TaskFerry inbox message as processed.",
    "parameters": obj({"message_id": string("Message id.")}, ["message_id"]),
}

TASKFERRY_LIST_TASKS = {
    "name": "taskferry_list_tasks",
    "description": "List local TaskFerry tasks.",
    "parameters": obj(),
}

TASKFERRY_ACCEPT_TASK = {
    "name": "taskferry_accept_task",
    "description": "Accept a TaskFerry task assigned to this agent.",
    "parameters": obj(
        {
            "task_id": string("Task id."),
            "from": string("Actor handle."),
            "message": string("Acceptance message."),
        },
        ["task_id", "from"],
    ),
}

TASKFERRY_DECLINE_TASK = {
    "name": "taskferry_decline_task",
    "description": "Decline a TaskFerry task assigned to this agent.",
    "parameters": obj(
        {
            "task_id": string("Task id."),
            "from": string("Actor handle."),
            "reason": string("Decline reason."),
        },
        ["task_id", "from"],
    ),
}

TASKFERRY_SUBMIT_ARTIFACT = {
    "name": "taskferry_submit_artifact",
    "description": "Submit an artifact for a TaskFerry task.",
    "parameters": obj(
        {
            "task_id": string("Task id."),
            "from": string("Actor handle."),
            "artifact_type": string("Artifact type, e.g. json, markdown, diff."),
            "content": {"description": "Artifact content."},
            "notes": string("Optional artifact notes."),
        },
        ["task_id", "from", "content"],
    ),
}

TASKFERRY_REQUEST_REVISION = {
    "name": "taskferry_request_revision",
    "description": "Request revision for a submitted TaskFerry artifact.",
    "parameters": obj(
        {
            "task_id": string("Task id."),
            "from": string("Actor handle."),
            "reason": string("Revision reason."),
            "requested_changes": array("Requested changes."),
        },
        ["task_id", "from", "reason"],
    ),
}

TASKFERRY_COMPLETE_TASK = {
    "name": "taskferry_complete_task",
    "description": "Complete a TaskFerry task after accepting the delivered artifact.",
    "parameters": obj(
        {
            "task_id": string("Task id."),
            "from": string("Actor handle."),
            "message": string("Completion message."),
        },
        ["task_id", "from"],
    ),
}
