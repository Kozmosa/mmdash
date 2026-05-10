"""add_team_id_to_provider_bindings

Revision ID: 002
Revises: 00c3f7399f9d
Create Date: 2026-05-09 10:00:00.000000

"""
from typing import Sequence, Union

from alembic import op
import sqlalchemy as sa


# revision identifiers, used by Alembic.
revision: str = '002'
down_revision: Union[str, None] = '00c3f7399f9d'
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    # Use batch mode for SQLite compatibility
    with op.batch_alter_table('provider_bindings', schema=None) as batch_op:
        batch_op.add_column(sa.Column('team_id', sa.String(36), nullable=True))
        batch_op.create_foreign_key('fk_provider_bindings_team_id', 'teams', ['team_id'], ['id'])


def downgrade() -> None:
    # Use batch mode for SQLite compatibility
    with op.batch_alter_table('provider_bindings', schema=None) as batch_op:
        batch_op.drop_constraint('fk_provider_bindings_team_id', type_='foreignkey')
        batch_op.drop_column('team_id')
