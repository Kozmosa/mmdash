from mmdash_worker.experiment import compare_results, summarize_result
from mmdash_worker.jobs.handlers import HandlerContext, HandlerError


def test_summarize_result_is_bounded_and_deterministic() -> None:
    result = summarize_result(HandlerContext("job", "worker"), {"experiment_id": "exp", "manifest": {"files": [{"path": "a.csv", "size_bytes": 3, "kind": "table"}]}})
    assert result["summary"] == "1 files, 3 bytes"
    assert len(result["summary_hash"]) == 64


def test_compare_requires_a_bounded_set() -> None:
    try:
        compare_results(HandlerContext("job", "worker"), {"items": []})
    except HandlerError as error:
        assert error.code == "COMPARISON_INPUT_INVALID"
    else:
        raise AssertionError("empty comparison was accepted")
