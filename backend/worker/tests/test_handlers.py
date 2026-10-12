import uuid

import pytest

from taskpilot_worker.errors import PermanentError, RetryableError
from taskpilot_worker.handlers.data import CsvSummaryV1, csv_summary
from taskpilot_worker.handlers.system import EchoV1, SleepV1, echo, sleep
from taskpilot_worker.handlers.text import WordCountV1, word_count
from taskpilot_worker.registry import HandlerContext

CTX = HandlerContext(job_id=uuid.uuid4(), attempt_no=1, deadline=0.0)


def test_echo_returns_message() -> None:
    assert echo(EchoV1(message="hi"), CTX) == {"message": "hi"}


def test_word_count_normalises_words() -> None:
    out = word_count(WordCountV1(text="Hello, world!\nhello WORLD -- ünïcode", top_n=2), CTX)
    assert out["words"] == 5
    assert out["lines"] == 2
    assert out["top_words"] == [{"word": "hello", "count": 2}, {"word": "world", "count": 2}]


def test_word_count_top_n_zero() -> None:
    assert word_count(WordCountV1(text="a b", top_n=0), CTX)["top_words"] == []


def test_csv_summary_numeric_and_text_columns() -> None:
    out = csv_summary(CsvSummaryV1(csv="a,b\n1,x\n3,y\n\n5,x\n"), CTX)
    assert out["rows"] == 3
    a, b = out["columns"]
    assert a == {
        "name": "a",
        "non_empty": 3,
        "distinct": 3,
        "numeric": True,
        "min": 1.0,
        "max": 5.0,
        "mean": 3.0,
    }
    assert b["numeric"] is False
    assert b["distinct"] == 2


def test_csv_summary_without_header_and_delimiter() -> None:
    out = csv_summary(CsvSummaryV1(csv="1;2\n3;4", has_header=False, delimiter=";"), CTX)
    assert [c["name"] for c in out["columns"]] == ["column_1", "column_2"]
    assert out["rows"] == 2


def test_csv_summary_non_finite_is_not_numeric() -> None:
    out = csv_summary(CsvSummaryV1(csv="a\n1\nnan\n"), CTX)
    assert out["columns"][0]["numeric"] is False


@pytest.mark.parametrize("text", ["a,b\n1,2,3\n", ",".join(["c"] * 101), 'a\n"unterminated'])
def test_csv_summary_rejects_bad_input(text: str) -> None:
    with pytest.raises(PermanentError) as exc:
        csv_summary(CsvSummaryV1(csv=text), CTX)
    assert exc.value.code == "csv_parse_error"


def test_sleep_fails_until_attempt() -> None:
    p = SleepV1(seconds=0, outcome="retryable_error", fail_until_attempt=2)
    with pytest.raises(RetryableError):
        sleep(p, CTX)
    assert sleep(p, HandlerContext(CTX.job_id, 2, 0.0))["attempt_no"] == 2


def test_sleep_permanent_error() -> None:
    with pytest.raises(PermanentError):
        sleep(SleepV1(seconds=0, outcome="permanent_error"), CTX)
