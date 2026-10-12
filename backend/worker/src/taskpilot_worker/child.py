"""Subprocess entry point: one attempt per process (ADR-0008).

Reads the envelope JSON from stdin and writes exactly one JSON line to stdout:
{"ok": true, "result": {...}} or {"ok": false, "error": {"code", "message", "retryable"}}.
"""

import json
import os
import sys
import time
from typing import Any

from pydantic import ValidationError

from taskpilot_worker.config import BUILTIN_APP
from taskpilot_worker.contract import Envelope
from taskpilot_worker.errors import TaskError
from taskpilot_worker.registry import HandlerContext, TaskRegistry, load_registry

if sys.platform == "linux":
    import resource

MEMORY_LIMIT_BYTES = 512 * 1024 * 1024  # Proposed limit (docs/03 §6.5)


def _error(code: str, message: str, retryable: bool) -> dict[str, Any]:
    return {"ok": False, "error": {"code": code, "message": message, "retryable": retryable}}


def run(raw: bytes, registry: TaskRegistry) -> dict[str, Any]:
    """Validate the envelope and payload, run the registered handler, and classify the outcome."""
    try:
        env = Envelope.model_validate_json(raw)
    except ValidationError:
        return _error("invalid_envelope", "envelope failed validation", retryable=False)
    spec = registry.get(env.task_type, env.task_version)
    if spec is None:
        return _error(
            "unsupported_task", f"{env.task_type} v{env.task_version} is not registered", False
        )
    try:
        payload = spec.payload_model.model_validate(env.payload)
    except ValidationError as e:
        # Schema drift between Go and Python: the payload will never become valid.
        return _error("invalid_payload", f"{e.error_count()} payload validation error(s)", False)
    ctx = HandlerContext(env.job_id, env.attempt_no, time.monotonic() + env.timeout_seconds)
    try:
        result = spec.handler(payload, ctx)
    except TaskError as e:
        return _error(e.code, e.message, e.retryable)
    except Exception as e:  # the one place a broad except is allowed (docs/10 §4)
        return _error("handler_error", f"unhandled {type(e).__name__}", retryable=True)
    return {"ok": True, "result": dict(result)}


def _limit_resources(raw: bytes) -> None:
    """Cap memory and CPU time on Linux. The parent's wall-clock timeout applies everywhere."""
    if sys.platform == "linux":
        try:
            timeout = int(json.loads(raw)["timeout_seconds"])
        except (ValueError, KeyError, TypeError):
            timeout = 900
        resource.setrlimit(resource.RLIMIT_AS, (MEMORY_LIMIT_BYTES, MEMORY_LIMIT_BYTES))
        resource.setrlimit(resource.RLIMIT_CPU, (timeout + 5, timeout + 5))


def main() -> None:
    """Run one attempt and print its outcome line."""
    # -I hides the parent's import paths; restore them so the application's modules import.
    extra = os.environ.get("TASKPILOT_CHILD_SYS_PATH", "").split(os.pathsep)
    sys.path[:0] = [p for p in extra if p and p not in sys.path]
    raw = sys.stdin.buffer.read()
    _limit_resources(raw)
    registry = load_registry(os.environ.get("TASKPILOT_WORKER_APP") or BUILTIN_APP)
    out = run(raw, registry)
    try:
        line = json.dumps(out, separators=(",", ":"), allow_nan=False)
    except (TypeError, ValueError):
        line = json.dumps(_error("invalid_result", "result is not JSON-serializable", False))
    sys.stdout.write(line + "\n")


if __name__ == "__main__":
    main()
