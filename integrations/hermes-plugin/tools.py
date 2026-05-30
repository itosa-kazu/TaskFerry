"""TaskFerry Hermes tool handlers."""

import json
import os
import urllib.error
import urllib.parse
import urllib.request


def _base_url():
    return os.environ.get("TASKFERRY_LOCAL_URL", "http://127.0.0.1:4318").rstrip("/")


def _token():
    return os.environ.get("TASKFERRY_LOCAL_API_TOKEN", "")


def _request(method, path, body=None):
    data = None
    headers = {}
    if body is not None:
        data = json.dumps(body).encode("utf-8")
        headers["Content-Type"] = "application/json"
    token = _token()
    if token:
        headers["Authorization"] = f"Bearer {token}"
    req = urllib.request.Request(_base_url() + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=15) as resp:
            raw = resp.read().decode("utf-8")
            return json.loads(raw) if raw else {}
    except urllib.error.HTTPError as exc:
        raw = exc.read().decode("utf-8", errors="replace")
        return {"ok": False, "error": f"TaskFerry local API {exc.code}: {raw}"}
    except Exception as exc:
        return {"ok": False, "error": str(exc)}


def _dump(value):
    return json.dumps(value, ensure_ascii=False)


def taskferry_health(args, **kwargs):
    del args, kwargs
    return _dump(_request("GET", "/health"))


def taskferry_register_agent(args, **kwargs):
    del kwargs
    return _dump(
        _request(
            "POST",
            "/agents",
            {
                "handle": args.get("handle", ""),
                "display_name": args.get("display_name", ""),
                "description": args.get("description", ""),
                "capabilities": args.get("capabilities") or [],
            },
        )
    )


def taskferry_request_connection(args, **kwargs):
    del kwargs
    return _dump(
        _request(
            "POST",
            "/connections/request",
            {"from": args.get("from", ""), "to": args.get("to", ""), "message": args.get("message", "")},
        )
    )


def taskferry_accept_connection(args, **kwargs):
    del kwargs
    return _dump(
        _request(
            "POST",
            "/connections/accept",
            {"from": args.get("from", ""), "to": args.get("to", "")},
        )
    )


def taskferry_create_task(args, **kwargs):
    del kwargs
    return _dump(
        _request(
            "POST",
            "/tasks",
            {
                "from": args.get("from", ""),
                "to": args.get("to", ""),
                "title": args.get("title", ""),
                "description": args.get("description", ""),
                "requirements": args.get("requirements") or [],
                "max_revisions": args.get("max_revisions", 3),
                "expected_format": args.get("expected_format", ""),
            },
        )
    )


def taskferry_check_inbox(args, **kwargs):
    del kwargs
    q = urllib.parse.urlencode(
        {
            "agent_id": args.get("agent", ""),
            "unprocessed": str(args.get("unprocessed", True)).lower(),
        }
    )
    return _dump(_request("GET", f"/inbox?{q}"))


def taskferry_ack_message(args, **kwargs):
    del kwargs
    message_id = urllib.parse.quote(args.get("message_id", ""), safe="")
    return _dump(_request("POST", f"/messages/{message_id}/ack", {}))


def taskferry_list_tasks(args, **kwargs):
    del args, kwargs
    return _dump(_request("GET", "/tasks"))


def taskferry_accept_task(args, **kwargs):
    del kwargs
    task_id = urllib.parse.quote(args.get("task_id", ""), safe="")
    return _dump(
        _request(
            "POST",
            f"/tasks/{task_id}/accept",
            {"from": args.get("from", ""), "message": args.get("message", "")},
        )
    )


def taskferry_decline_task(args, **kwargs):
    del kwargs
    task_id = urllib.parse.quote(args.get("task_id", ""), safe="")
    return _dump(
        _request(
            "POST",
            f"/tasks/{task_id}/decline",
            {"from": args.get("from", ""), "reason": args.get("reason", "")},
        )
    )


def taskferry_submit_artifact(args, **kwargs):
    del kwargs
    task_id = urllib.parse.quote(args.get("task_id", ""), safe="")
    return _dump(
        _request(
            "POST",
            f"/tasks/{task_id}/artifacts",
            {
                "from": args.get("from", ""),
                "artifact_type": args.get("artifact_type", "json"),
                "content": args.get("content", {}),
                "notes": args.get("notes", ""),
            },
        )
    )


def taskferry_request_revision(args, **kwargs):
    del kwargs
    task_id = urllib.parse.quote(args.get("task_id", ""), safe="")
    return _dump(
        _request(
            "POST",
            f"/tasks/{task_id}/revision",
            {
                "from": args.get("from", ""),
                "reason": args.get("reason", ""),
                "requested_changes": args.get("requested_changes") or [],
            },
        )
    )


def taskferry_complete_task(args, **kwargs):
    del kwargs
    task_id = urllib.parse.quote(args.get("task_id", ""), safe="")
    return _dump(
        _request(
            "POST",
            f"/tasks/{task_id}/complete",
            {"from": args.get("from", ""), "message": args.get("message", "")},
        )
    )
