"""Worker settings from the environment. The worker has no TaskPilot database settings
(ADR-0004)."""

import os
from collections.abc import Mapping
from dataclasses import dataclass, field

BUILTIN_APP = "taskpilot_worker.handlers.builtin:registry"


def _csv(value: str) -> tuple[str, ...]:
    return tuple(v.strip() for v in value.split(",") if v.strip())


@dataclass(frozen=True, slots=True)
class Config:
    """Immutable worker configuration."""

    api_url: str
    worker_token: str = field(repr=False)
    app: str = BUILTIN_APP
    queues: tuple[str, ...] = ()  # empty = every queue the app's tasks route to
    child_env_allow: tuple[str, ...] = ()
    concurrency: int = 4
    poll_interval: float = 1.0
    shutdown_grace: float = 25.0

    @classmethod
    def from_env(cls, env: Mapping[str, str] = os.environ) -> "Config":
        """Build a Config; errors name the variable, never a secret value."""
        missing = [
            n for n in ("TASKPILOT_API_INTERNAL_URL", "TASKPILOT_WORKER_TOKEN") if not env.get(n)
        ]
        if missing:
            raise ValueError(f"missing required settings: {', '.join(missing)}")
        cfg = cls(
            api_url=env["TASKPILOT_API_INTERNAL_URL"],
            worker_token=env["TASKPILOT_WORKER_TOKEN"],
            app=env.get("TASKPILOT_WORKER_APP") or BUILTIN_APP,
            queues=_csv(env.get("TASKPILOT_WORKER_QUEUES", "")),
            child_env_allow=_csv(env.get("TASKPILOT_WORKER_CHILD_ENV", "")),
            concurrency=int(env.get("TASKPILOT_WORKER_CONCURRENCY", "4")),
            poll_interval=float(env.get("TASKPILOT_WORKER_POLL_INTERVAL_SECONDS", "1")),
            shutdown_grace=float(env.get("TASKPILOT_WORKER_SHUTDOWN_GRACE_SECONDS", "25")),
        )
        if cfg.concurrency < 1:
            raise ValueError("TASKPILOT_WORKER_CONCURRENCY must be >= 1")
        if any(name.startswith("TASKPILOT_WORKER_TOKEN") for name in cfg.child_env_allow):
            raise ValueError("TASKPILOT_WORKER_CHILD_ENV must not include the worker token")
        return cfg
