"""Entry point for the built-in task types:
TASKPILOT_WORKER_APP=taskpilot_worker.handlers.builtin:registry.
"""

from taskpilot_worker.handlers import data, system, text
from taskpilot_worker.handlers.base import registry

__all__ = ["data", "registry", "system", "text"]
