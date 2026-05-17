import asyncio
import logging
from datetime import datetime, timedelta

from sqlalchemy.orm import Session

from app.database import SessionLocal
from app.models import TimelineEvent, Todo

logger = logging.getLogger(__name__)

INTERVAL_SECONDS = 30
DETECTION_WINDOW_SECONDS = 35

_stop_event = asyncio.Event()


def _check_reminders(db: Session, now: datetime | None = None) -> tuple[list[TimelineEvent], list[Todo]]:
    """Check for reminders due within the detection window.

    Marks matching records with reminder_detected=True.
    Returns (events, todos) lists of detected records.
    """
    if now is None:
        now = datetime.utcnow()
    window_start = now - timedelta(seconds=INTERVAL_SECONDS)
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
            if window_start <= reminder_time <= window_end:
                e.reminder_detected = True
                detected_events.append(e)

    # Todo: reminder_at is an absolute timestamp
    todos = db.query(Todo).filter(
        Todo.reminder_enabled == True,
        Todo.reminder_detected == False,
        Todo.reminder_at.isnot(None),
        Todo.reminder_at.between(window_start, window_end),
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
    while not _stop_event.is_set():
        try:
            db = SessionLocal()
            try:
                events, todos = _check_reminders(db)
                if events or todos:
                    asyncio.create_task(_dispatch_im_notifications(db, events, todos))
            finally:
                db.close()
        except Exception:
            logger.exception("Reminder check failed")
        try:
            await asyncio.wait_for(
                _stop_event.wait(), timeout=INTERVAL_SECONDS
            )
        except asyncio.TimeoutError:
            pass  # Normal tick — no stop signal, continue loop


async def _dispatch_im_notifications(db, events: list, todos: list):
    """Send IM notifications for detected reminders via all configured IM providers."""
    from app.services.im_provider import get_im_providers
    from app.models import IMUserBinding, IMProjectBinding

    providers = get_im_providers()
    if not providers:
        return

    # Collect unique project_ids and user_ids
    project_ids = set()
    user_ids = set()
    for e in events:
        project_ids.add(e.project_id)
        user_ids.add(e.user_id)
    for t in todos:
        project_ids.add(t.project_id)
        user_ids.add(t.user_id)

    # Prefetch bindings
    user_bindings = {
        b.user_id: b
        for b in db.query(IMUserBinding).filter(
            IMUserBinding.user_id.in_(user_ids),
            IMUserBinding.enabled == True,
        ).all()
    }
    project_bindings = {
        b.project_id: b
        for b in db.query(IMProjectBinding).filter(
            IMProjectBinding.project_id.in_(project_ids),
            IMProjectBinding.enabled == True,
        ).all()
    }

    msg_format_event = "📅 日程提醒\n{title}\n开始时间: {start_time}"
    msg_format_todo = "✅ 待办提醒\n{content}\n截止时间: {due_date}"

    for provider in providers:
        for e in events:
            user_binding = user_bindings.get(e.user_id)
            project_binding = project_bindings.get(e.project_id)

            start_str = e.start_time.strftime("%Y-%m-%d %H:%M") if e.start_time else "未设置"
            title = msg_format_event.format(title=e.title, start_time=start_str)
            body = e.description or ""

            # Personal message
            if user_binding and user_binding.im_user_id:
                await _safe_send(provider, "user", user_binding.im_user_id, title, body)

            # Group message for team events
            if e.is_team_event and project_binding and project_binding.im_chat_id:
                await _safe_send(provider, "chat", project_binding.im_chat_id, title, body)

        for t in todos:
            user_binding = user_bindings.get(t.user_id)
            project_binding = project_bindings.get(t.project_id)

            due_str = t.due_date.strftime("%Y-%m-%d %H:%M") if t.due_date else "未设置"
            title = msg_format_todo.format(content=t.content, due_date=due_str)
            body = ""

            if user_binding and user_binding.im_user_id:
                await _safe_send(provider, "user", user_binding.im_user_id, title, body)

            if t.is_team_todo and project_binding and project_binding.im_chat_id:
                await _safe_send(provider, "chat", project_binding.im_chat_id, title, body)


async def _safe_send(provider, recipient_type: str, recipient_id: str, title: str, body: str):
    """Send a message, catching all exceptions so one failure doesn't block others."""
    try:
        await provider.send_message(recipient_type, recipient_id, title, body)
    except Exception:
        logger.exception("IM send failed for %s:%s", recipient_type, recipient_id)


async def start_reminder_scheduler():
    await _run_loop()


def stop_reminder_scheduler():
    _stop_event.set()
