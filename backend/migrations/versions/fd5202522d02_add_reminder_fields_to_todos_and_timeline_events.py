"""add reminder fields to todos and timeline_events

Revision ID: fd5202522d02
Revises: a0b22258ba0a
Create Date: 2026-05-18 00:11:09.040166

"""
from typing import Sequence, Union

from alembic import op
import sqlalchemy as sa


# revision identifiers, used by Alembic.
revision: str = 'fd5202522d02'
down_revision: Union[str, None] = 'a0b22258ba0a'
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    with op.batch_alter_table('timeline_events', schema=None) as batch_op:
        batch_op.add_column(sa.Column('reminder_enabled', sa.Boolean(), nullable=False, server_default=sa.text("0")))
        batch_op.add_column(sa.Column('reminder_minutes_before', sa.Integer(), nullable=True))
        batch_op.add_column(sa.Column('reminder_detected', sa.Boolean(), nullable=False, server_default=sa.text("0")))
        batch_op.add_column(sa.Column('reminder_acked', sa.Boolean(), nullable=False, server_default=sa.text("0")))

    with op.batch_alter_table('todos', schema=None) as batch_op:
        batch_op.add_column(sa.Column('reminder_enabled', sa.Boolean(), nullable=False, server_default=sa.text("0")))
        batch_op.add_column(sa.Column('reminder_at', sa.DateTime(), nullable=True))
        batch_op.add_column(sa.Column('reminder_detected', sa.Boolean(), nullable=False, server_default=sa.text("0")))
        batch_op.add_column(sa.Column('reminder_acked', sa.Boolean(), nullable=False, server_default=sa.text("0")))


def downgrade() -> None:
    with op.batch_alter_table('todos', schema=None) as batch_op:
        batch_op.drop_column('reminder_acked')
        batch_op.drop_column('reminder_detected')
        batch_op.drop_column('reminder_at')
        batch_op.drop_column('reminder_enabled')

    with op.batch_alter_table('timeline_events', schema=None) as batch_op:
        batch_op.drop_column('reminder_acked')
        batch_op.drop_column('reminder_detected')
        batch_op.drop_column('reminder_minutes_before')
        batch_op.drop_column('reminder_enabled')
