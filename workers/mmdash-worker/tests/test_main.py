from mmdash_worker.main import status


def test_status_identifies_worker() -> None:
    assert status() == {
        "service": "mmdash-worker",
        "version": "0.1.0",
        "status": "ready",
    }
