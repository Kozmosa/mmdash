"""add citations and zotero_configs

Revision ID: a0b22258ba0a
Revises: 003_add_llm_prompts_to_teams
Create Date: 2026-05-15 11:24:40.027618

"""
from typing import Sequence, Union

from alembic import op
import sqlalchemy as sa


# revision identifiers, used by Alembic.
revision: str = 'a0b22258ba0a'
down_revision: Union[str, None] = '003_add_llm_prompts_to_teams'
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    op.create_table(
        'citations',
        sa.Column('id', sa.String(36), primary_key=True),
        sa.Column('project_id', sa.String(36), sa.ForeignKey('projects.id'), nullable=False),
        sa.Column('user_id', sa.String(36), sa.ForeignKey('users.id'), nullable=False),
        sa.Column('title', sa.String(500), nullable=False),
        sa.Column('authors', sa.Text(), nullable=True),
        sa.Column('journal', sa.String(255), nullable=True),
        sa.Column('year', sa.Integer(), nullable=True),
        sa.Column('volume', sa.String(50), nullable=True),
        sa.Column('issue', sa.String(50), nullable=True),
        sa.Column('pages', sa.String(100), nullable=True),
        sa.Column('doi', sa.String(255), nullable=True, index=True),
        sa.Column('url', sa.String(500), nullable=True),
        sa.Column('abstract', sa.Text(), nullable=True),
        sa.Column('bibtex_key', sa.String(100), nullable=True),
        sa.Column('bibtex_type', sa.String(50), nullable=True, server_default='article'),
        sa.Column('zotero_item_key', sa.String(50), nullable=True, index=True),
        sa.Column('zotero_version', sa.Integer(), nullable=True),
        sa.Column('source', sa.String(20), nullable=True, server_default='manual'),
        sa.Column('extra_data', sa.Text(), nullable=True),
        sa.Column('created_at', sa.DateTime(), nullable=True, server_default=sa.text('CURRENT_TIMESTAMP')),
        sa.Column('updated_at', sa.DateTime(), nullable=True, server_default=sa.text('CURRENT_TIMESTAMP')),
    )

    op.create_table(
        'zotero_configs',
        sa.Column('id', sa.String(36), primary_key=True),
        sa.Column('project_id', sa.String(36), sa.ForeignKey('projects.id'), nullable=False, unique=True),
        sa.Column('api_key', sa.String(255), nullable=False),
        sa.Column('library_id', sa.String(50), nullable=False),
        sa.Column('library_type', sa.String(20), nullable=True, server_default='user'),
        sa.Column('last_sync_version', sa.Integer(), nullable=True),
        sa.Column('last_sync_at', sa.DateTime(), nullable=True),
        sa.Column('last_sync_status', sa.String(20), nullable=True, server_default='idle'),
        sa.Column('last_sync_error', sa.Text(), nullable=True),
        sa.Column('created_at', sa.DateTime(), nullable=True, server_default=sa.text('CURRENT_TIMESTAMP')),
    )


def downgrade() -> None:
    op.drop_table('zotero_configs')
    op.drop_table('citations')
