import { defineToolPlugin } from "openclaw/plugin-sdk/tool-plugin";
import { Type } from "typebox";

const DEFAULT_BASE_URL = "http://127.0.0.1:4318";

function baseUrl() {
  return (process.env.TASKFERRY_LOCAL_URL || DEFAULT_BASE_URL).replace(/\/+$/, "");
}

function localToken() {
  return process.env.TASKFERRY_LOCAL_API_TOKEN || "";
}

async function request(method, path, body, signal) {
  const headers = {};
  const token = localToken();
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  const init = { method, headers, signal };
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }

  const response = await fetch(baseUrl() + path, init);
  const text = await response.text();
  if (!response.ok) {
    throw new Error(`TaskFerry local API ${response.status}: ${text}`);
  }
  return text ? JSON.parse(text) : {};
}

async function call(method, path, body, context) {
  context?.signal?.throwIfAborted?.();
  return request(method, path, body, context?.signal);
}

const string = (description = "") => Type.String({ description });
const array = (description = "") => Type.Array(Type.String(), { description });
const integer = (description = "") => Type.Integer({ description });
const boolean = (description = "") => Type.Boolean({ description });
const artifactContent = () => Type.Any({ description: "Artifact content." });

function object(properties = {}, required = []) {
  const requiredSet = new Set(required);
  const shaped = {};
  for (const [key, schema] of Object.entries(properties)) {
    shaped[key] = requiredSet.has(key) ? schema : Type.Optional(schema);
  }
  return Type.Object(shaped, { additionalProperties: false });
}

export default defineToolPlugin({
  id: "taskferry",
  name: "TaskFerry",
  description: "Private task delivery tools for local AI agents.",
  configSchema: object(),
  tools: (tool) => [
    tool({
      name: "taskferry_health",
      label: "TaskFerry Health",
      description: "Check the local TaskFerry client health and relay connection state.",
      parameters: object(),
      execute: (_params, _config, context) => call("GET", "/health", undefined, context),
    }),
    tool({
      name: "taskferry_register_agent",
      label: "Register TaskFerry Agent",
      description: "Register or update a local TaskFerry agent handle.",
      parameters: object(
        {
          handle: string("Agent handle, e.g. @alice/worker."),
          display_name: string("Human-readable display name."),
          description: string("Agent description."),
          capabilities: array("Agent capability tags."),
        },
        ["handle"],
      ),
      execute: (params, _config, context) =>
        call(
          "POST",
          "/agents",
          {
            handle: params.handle,
            display_name: params.display_name || "",
            description: params.description || "",
            capabilities: params.capabilities || [],
          },
          context,
        ),
    }),
    tool({
      name: "taskferry_request_connection",
      label: "Request TaskFerry Connection",
      description: "Request approval to communicate with another TaskFerry agent.",
      parameters: object(
        {
          from: string("Sender handle."),
          to: string("Recipient handle."),
          message: string("Request message."),
        },
        ["from", "to"],
      ),
      execute: (params, _config, context) => call("POST", "/connections/request", params, context),
    }),
    tool({
      name: "taskferry_accept_connection",
      label: "Accept TaskFerry Connection",
      description: "Accept a TaskFerry connection request.",
      parameters: object(
        {
          from: string("Accepting agent handle."),
          to: string("Requester handle."),
        },
        ["from", "to"],
      ),
      execute: (params, _config, context) => call("POST", "/connections/accept", params, context),
    }),
    tool({
      name: "taskferry_create_task",
      label: "Create TaskFerry Task",
      description: "Create a typed TaskFerry task for another agent.",
      parameters: object(
        {
          from: string("Requester handle."),
          to: string("Assignee handle."),
          title: string("Task title."),
          description: string("Task details."),
          requirements: array("Acceptance requirements."),
          max_revisions: integer("Maximum revisions."),
          expected_format: string("Expected artifact format."),
        },
        ["from", "to", "title", "description"],
      ),
      execute: (params, _config, context) =>
        call(
          "POST",
          "/tasks",
          {
            ...params,
            requirements: params.requirements || [],
            max_revisions: params.max_revisions ?? 3,
          },
          context,
        ),
    }),
    tool({
      name: "taskferry_check_inbox",
      label: "Check TaskFerry Inbox",
      description: "Read a local TaskFerry agent inbox.",
      parameters: object(
        {
          agent: string("Agent handle."),
          unprocessed: boolean("Only unprocessed messages."),
        },
        ["agent"],
      ),
      execute: (params, _config, context) => {
        const q = new URLSearchParams({
          agent_id: params.agent,
          unprocessed: String(params.unprocessed ?? true),
        });
        return call("GET", `/inbox?${q.toString()}`, undefined, context);
      },
    }),
    tool({
      name: "taskferry_ack_message",
      label: "Ack TaskFerry Message",
      description: "Mark a local TaskFerry inbox message as processed.",
      parameters: object({ message_id: string("Message id.") }, ["message_id"]),
      execute: (params, _config, context) =>
        call("POST", `/messages/${encodeURIComponent(params.message_id)}/ack`, {}, context),
    }),
    tool({
      name: "taskferry_list_tasks",
      label: "List TaskFerry Tasks",
      description: "List local TaskFerry tasks.",
      parameters: object(),
      execute: (_params, _config, context) => call("GET", "/tasks", undefined, context),
    }),
    tool({
      name: "taskferry_accept_task",
      label: "Accept TaskFerry Task",
      description: "Accept a TaskFerry task assigned to this agent.",
      parameters: object(
        {
          task_id: string("Task id."),
          from: string("Actor handle."),
          message: string("Acceptance message."),
        },
        ["task_id", "from"],
      ),
      execute: (params, _config, context) =>
        call(
          "POST",
          `/tasks/${encodeURIComponent(params.task_id)}/accept`,
          {
            from: params.from,
            message: params.message || "",
          },
          context,
        ),
    }),
    tool({
      name: "taskferry_decline_task",
      label: "Decline TaskFerry Task",
      description: "Decline a TaskFerry task assigned to this agent.",
      parameters: object(
        {
          task_id: string("Task id."),
          from: string("Actor handle."),
          reason: string("Decline reason."),
        },
        ["task_id", "from"],
      ),
      execute: (params, _config, context) =>
        call(
          "POST",
          `/tasks/${encodeURIComponent(params.task_id)}/decline`,
          {
            from: params.from,
            reason: params.reason || "",
          },
          context,
        ),
    }),
    tool({
      name: "taskferry_submit_artifact",
      label: "Submit TaskFerry Artifact",
      description: "Submit an artifact for a TaskFerry task.",
      parameters: object(
        {
          task_id: string("Task id."),
          from: string("Actor handle."),
          artifact_type: string("Artifact type, e.g. json, markdown, diff."),
          content: artifactContent(),
          notes: string("Optional artifact notes."),
        },
        ["task_id", "from", "content"],
      ),
      execute: (params, _config, context) =>
        call(
          "POST",
          `/tasks/${encodeURIComponent(params.task_id)}/artifacts`,
          {
            from: params.from,
            artifact_type: params.artifact_type || "json",
            content: params.content,
            notes: params.notes || "",
          },
          context,
        ),
    }),
    tool({
      name: "taskferry_request_revision",
      label: "Request TaskFerry Revision",
      description: "Request revision for a submitted TaskFerry artifact.",
      parameters: object(
        {
          task_id: string("Task id."),
          from: string("Actor handle."),
          reason: string("Revision reason."),
          requested_changes: array("Requested changes."),
        },
        ["task_id", "from", "reason"],
      ),
      execute: (params, _config, context) =>
        call(
          "POST",
          `/tasks/${encodeURIComponent(params.task_id)}/revision`,
          {
            from: params.from,
            reason: params.reason,
            requested_changes: params.requested_changes || [],
          },
          context,
        ),
    }),
    tool({
      name: "taskferry_complete_task",
      label: "Complete TaskFerry Task",
      description: "Complete a TaskFerry task after accepting the delivered artifact.",
      parameters: object(
        {
          task_id: string("Task id."),
          from: string("Actor handle."),
          message: string("Completion message."),
        },
        ["task_id", "from"],
      ),
      execute: (params, _config, context) =>
        call(
          "POST",
          `/tasks/${encodeURIComponent(params.task_id)}/complete`,
          {
            from: params.from,
            message: params.message || "",
          },
          context,
        ),
    }),
  ],
});
