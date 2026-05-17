import asyncio
import logging
from datetime import datetime, timedelta

from app.database import SessionLocal
from app.models import TimelineEvent, Todo

logger = logging.getLogger(__name__)

INTERVAL_SECONDS = 30
DETECTION_WINDOW_SECONDS = 35

_stop_event: asyncio.Event | None = None


def _check_reminders(db, now=None):
    """Check for reminders due within the detection window.
    Marks matching records with reminder_detected=True.
    Returns (events, todos) lists of detected records.
    """
    if now is None:
        now = datetime.utcnow()
    window_end = now + timedelta(seconds=DETECTION_WINDOW_SECONDS)

    # TimelineEvent: computed reminder time = start_time - reminder_minutes_before
    events = db.query(TimelineEvent).filter(
        TimelineEvent.reminder_enabled == True,
        TimelineEvent.reminder_detected == False,
        TimelineEvent.reminder_minutes_before.isnot(None),
    ).all()

    detected_events = []
    for e in events:
        if e.reminder_minutes_before is not None:
            reminder_time = e.start_time - timedelta(minutes=e.reminder_minutes_before)
            if now <= reminder_time <= window_end:
                e.reminder_detected = True
                detected_events.append(e)

    # Todo: reminder_at is an absolute timestamp
    todos = db.query(Todo).filter(
        Todo.reminder_enabled == True,
        Todo.reminder_detected == False,
        Todo.reminder_at.isnot(None),
        Todo.reminder_at.between(now, window_end),
    ).all()

    for t in todos:
        t.reminder_detected = True

    if detected_events or todos:
        db.commit()
        logger.info(
            "Reminders detected: %d events, %d todos",
            len(detected_events), len(todos),
        )

    return detected_events, todos


async def _run_loop():
    global _stop_event
    _stop_event = asyncio.Event()
    while not _stop_event.is_set():
        try:
            db = SessionLocal()
            try:
                _check_reminders(db)
            finally:
                db.close()
        except Exception:
            logger.exception("Reminder check failed")
        await asyncio.wait_for(
            _stop_event.wait(), timeout=INTERVAL_SECONDS
        )


def start_reminder_scheduler():
    global _stop_event
    _stop_event = asyncio.Event()
    return asyncio.create_task(_run_loop())


def stop_reminder_scheduler():
    global _stop_event
    if _stop_event:
        _stop_event.set()
