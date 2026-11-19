"""data.* task types."""

import csv
import io
import math
import statistics
from typing import Any, Literal

from pydantic import Field

from taskpilot_worker.errors import PermanentError
from taskpilot_worker.handlers.base import registry
from taskpilot_worker.registry import HandlerContext, Payload

MAX_COLUMNS = 100


class CsvSummaryV1(Payload):
    """data.csv_summary v1 payload."""

    csv: str = Field(
        min_length=1,
        max_length=60000,
        description="The CSV text, up to 60,000 characters and 100 columns.",
    )
    has_header: bool = Field(default=True, description="Treat the first row as column names.")
    delimiter: Literal[",", ";", "\t", "|"] = Field(
        default=",", description="The character between values."
    )


@registry.task(
    "data.csv_summary",
    title="CSV summary",
    timeout=30,
    max_timeout=120,
    example={"csv": "city,temperature\nPune,31\nDelhi,35\nMumbai,30", "has_header": True},
)
def csv_summary(p: CsvSummaryV1, _ctx: HandlerContext) -> dict[str, Any]:
    """Summarises a CSV table: the number of rows and, for every column, how many values are filled
    in,
    how many are distinct, and the minimum, maximum, and average when the column is numeric. Badly
    formed CSV fails straight away without retrying.
    """
    try:
        rows = [r for r in csv.reader(io.StringIO(p.csv), delimiter=p.delimiter, strict=True) if r]
    except csv.Error as e:
        raise PermanentError("csv_parse_error", f"malformed CSV: {e}") from e
    if not rows:
        raise PermanentError("csv_parse_error", "no rows")
    width = len(rows[0])
    if width > MAX_COLUMNS:
        raise PermanentError("csv_parse_error", f"more than {MAX_COLUMNS} columns")
    if ragged := next((i for i, r in enumerate(rows, 1) if len(r) != width), None):
        raise PermanentError("csv_parse_error", f"row {ragged} has a different number of fields")

    names = rows[0] if p.has_header else [f"column_{i}" for i in range(1, width + 1)]
    data = rows[1:] if p.has_header else rows
    return {
        "rows": len(data),
        "columns": [_column(name, [r[i] for r in data]) for i, name in enumerate(names)],
    }


def _column(name: str, values: list[str]) -> dict[str, Any]:
    present = [v for v in values if v.strip()]
    numbers = _numbers(present)
    return {
        "name": name,
        "non_empty": len(present),
        "distinct": len(set(present)),
        "numeric": numbers is not None,
        "min": min(numbers) if numbers else None,
        "max": max(numbers) if numbers else None,
        "mean": statistics.fmean(numbers) if numbers else None,
    }


def _numbers(values: list[str]) -> list[float] | None:
    """Parse every value as a finite number, or return None if any is not one."""
    try:
        numbers = [float(v) for v in values]
    except ValueError:
        return None
    return numbers if numbers and all(map(math.isfinite, numbers)) else None
