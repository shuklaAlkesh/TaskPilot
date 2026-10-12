"""Wire models for the Go worker API. Source of truth: backend/contracts/job.schema.json."""

from datetime import datetime
from typing import Any, Literal
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field

ERROR_CODE_PATTERN = r"^[a-z][a-z0-9_]{0,63}$"
MAX_RESULT_BYTES = 64 * 1024


class _Wire(BaseModel):
    # Additive fields from a newer server are ignored, so deploys can be rolled one side at a time.
    model_config = ConfigDict(frozen=True, extra="ignore")


class Envelope(_Wire):
    """The job handed to a worker by a claim."""

    schema_version: Literal[1]
    job_id: UUID
    attempt_id: UUID
    attempt_no: int = Field(ge=1, le=10)
    max_attempts: int = Field(ge=1, le=10)
    task_type: str
    task_version: int = Field(ge=1)
    timeout_seconds: int = Field(ge=1, le=86_400)  # up to 24 h (ADR-0009)
    payload: dict[str, Any]
    created_at: datetime


class Claim(_Wire):
    """Claim response: the leased attempt and its lease timing."""

    lease_seconds: int = Field(ge=1)
    heartbeat_interval_seconds: int = Field(ge=1)
    job: Envelope


class Lease(_Wire):
    """Heartbeat response."""

    lease_seconds: int
    cancel_requested: bool


class ErrorInfo(_Wire):
    """A classified failure reported to Go. The message never contains payload data."""

    code: str = Field(pattern=ERROR_CODE_PATTERN)
    message: str = Field(max_length=1000)
    retryable: bool


class ChildOutput(_Wire):
    """The single JSON line a handler subprocess writes to stdout."""

    ok: bool
    result: dict[str, Any] | None = None
    error: ErrorInfo | None = None
