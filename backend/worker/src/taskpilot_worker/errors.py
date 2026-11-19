"""Errors handlers raise to classify a failure. Go decides what happens next (docs/03 §6.3)."""


class TaskError(Exception):
    """Base class for classified handler failures."""

    retryable: bool = False

    def __init__(self, code: str, message: str) -> None:
        super().__init__(message)
        self.code = code
        self.message = message


class RetryableError(TaskError):
    """A transient failure: the job may succeed if retried."""

    retryable = True


class PermanentError(TaskError):
    """A failure that will repeat on every attempt, such as invalid input."""

    retryable = False
