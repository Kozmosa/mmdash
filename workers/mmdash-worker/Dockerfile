FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    UV_INDEX_URL=https://pypi.tuna.tsinghua.edu.cn/simple \
    UV_LINK_MODE=copy

RUN pip install --no-cache-dir \
    --index-url https://pypi.tuna.tsinghua.edu.cn/simple \
    uv==0.8.22

WORKDIR /app

COPY pyproject.toml uv.lock ./
COPY workers/mmdash-worker workers/mmdash-worker

RUN uv sync --frozen --no-dev --package mmdash-worker

ENV PATH="/app/.venv/bin:${PATH}"

USER 65532:65532

ENTRYPOINT ["mmdash-worker"]
