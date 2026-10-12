"""system.* task types: a smoke test, and a test-only sleep used to demonstrate failures."""

import os
import time
from typing import Any, Literal

from pydantic import Field

from taskpilot_worker.errors import PermanentError, RetryableError
from taskpilot_worker.handlers.base import registry
from taskpilot_worker.registry import HandlerContext, Payload

CRASH_EXIT_CODE = 70


class EchoV1(Payload):
    """system.echo v1 payload."""

    message: str = Field(
        min_length=1,
        max_length=1000,
        description="Any text, 1-1,000 characters. It is returned as the result.",
    )


@registry.task(
    "system.echo",
    title="Echo",
    timeout=10,
    max_timeout=30,
    example={"message": "Hello, TaskPilot!"},
)
def echo(p: EchoV1, _ctx: HandlerContext) -> dict[str, Any]:
    """Sends your message back unchanged. The quickest way to check that the API, queue, and workers
    are all working.
    """
    return {"message": p.message}


class SleepV1(Payload):
    """system.sleep v1 payload (test only)."""

    seconds: float = Field(ge=0, le=600, description="How long to wait, 0-600 seconds.")
    outcome: Literal["succeed", "retryable_error", "permanent_error", "crash"] = Field(
        default="succeed",
        description="What happens after waiting: succeed, a retryable error, a permanent error, "
        "or a crash.",
    )
    fail_until_attempt: int | None = Field(
        default=None,
        ge=1,
        le=10,
        description="Apply the outcome only while the attempt number is below this, then succeed. "
        "2 = fail once, then succeed on the retry.",
    )


@registry.task(
    "system.sleep",
    title="Sleep",
    timeout=60,
    max_timeout=900,
    test_only=True,
    example={"seconds": 5, "outcome": "retryable_error", "fail_until_attempt": 2},
)
def sleep(p: SleepV1, ctx: HandlerContext) -> dict[str, Any]:
    """Waits for a number of seconds, then succeeds or fails the way you ask. Use it to watch
    timeouts,
    retries, cancellation, and the dead letter in action. Only available when test tasks are
    enabled.
    """
    time.sleep(p.seconds)
    failing = p.fail_until_attempt is None or ctx.attempt_no < p.fail_until_attempt
    if p.outcome == "succeed" or not failing:
        return {"slept_seconds": p.seconds, "attempt_no": ctx.attempt_no}
    if p.outcome == "retryable_error":
        raise RetryableError("sleep_retryable", "requested retryable failure")
    if p.outcome == "permanent_error":
        raise PermanentError("sleep_permanent", "requested permanent failure")
    os._exit(CRASH_EXIT_CODE)  # simulate a hard crash; only ever runs inside the child process
