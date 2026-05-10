"""add llm prompts to teams

Revision ID: 003_add_llm_prompts_to_teams
Revises: 002_add_team_id_to_provider_bindings
Create Date: 2026-05-09 23:30:00.000000
"""

from alembic import op
import sqlalchemy as sa


# revision identifiers, used by Alembic.
revision = "003_add_llm_prompts_to_teams"
down_revision = "002"
branch_labels = None
depends_on = None


def upgrade() -> None:
    with op.batch_alter_table("teams") as batch_op:
        batch_op.add_column(sa.Column("llm_prompts", sa.Text(), nullable=False, server_default="{}"))


def downgrade() -> None:
    with op.batch_alter_table("teams") as batch_op:
        batch_op.drop_column("llm_prompts")
