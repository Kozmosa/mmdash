ARG PYTHON_BASE_IMAGE=python:3.12.11-slim-bookworm

FROM ${PYTHON_BASE_IMAGE}

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    UV_INDEX_URL=https://pypi.tuna.tsinghua.edu.cn/simple \
    UV_LINK_MODE=copy \
    SOURCE_DATE_EPOCH=0 \
    TZ=UTC \
    TEXMFVAR=/tmp/texmf-var \
    TEXMFCONFIG=/tmp/texmf-config \
    TEXMFHOME=/tmp/texmf-home

ARG DEBIAN_MIRROR=http://deb.debian.org/debian
ARG DEBIAN_SECURITY_MIRROR=http://deb.debian.org/debian-security

# Article builds use an explicitly pinned Debian bookworm toolchain. Worker
# startup/build validation rejects a binary version that drifts from the Core
# job grant, and every successful Build records the complete version lines.
RUN sed -i \
      -e "s|http://deb.debian.org/debian-security|${DEBIAN_SECURITY_MIRROR}|g" \
      -e "s|http://deb.debian.org/debian|${DEBIAN_MIRROR}|g" \
      /etc/apt/sources.list.d/debian.sources \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
      biber=2.18-1 \
      latexmk=1:4.79-1 \
      pandoc=2.17.1.1-2~deb12u1 \
      texlive-fonts-recommended=2022.20230122-3 \
      texlive-latex-base=2022.20230122-3 \
      texlive-latex-extra=2022.20230122-4 \
      texlive-latex-recommended=2022.20230122-3 \
      texlive-lang-chinese=2022.20230122-1 \
      texlive-luatex=2022.20230122-3 \
      texlive-xetex=2022.20230122-3 \
    && pandoc --version | grep -F 'pandoc 2.17.1.1' \
    && latexmk -v | grep -F 'Version 4.79' \
    && pdflatex --version | grep -F 'TeX Live 2022/Debian' \
    && xelatex --version | grep -F 'TeX Live 2022/Debian' \
    && lualatex --version | grep -F 'TeX Live 2022/Debian' \
    && kpsewhich ctexart.cls | grep -F '/ctex/ctexart.cls' \
    && rm -rf /var/lib/apt/lists/*

ARG PYPI_INDEX_URL=https://mirrors.aliyun.com/pypi/simple/
ENV UV_DEFAULT_INDEX=${PYPI_INDEX_URL} \
    UV_INDEX_URL=${PYPI_INDEX_URL}

RUN pip install --no-cache-dir \
    --index-url "${PYPI_INDEX_URL}" \
    uv==0.8.22

WORKDIR /app

COPY pyproject.toml uv.lock ./
COPY workers/mmdash-worker workers/mmdash-worker

RUN uv sync --frozen --no-dev --package mmdash-worker
RUN chmod -R a+rX workers/mmdash-worker
RUN mkdir -p /tmp/texmf-var /tmp/texmf-config /tmp/texmf-home \
    && chmod 1777 /tmp/texmf-var /tmp/texmf-config /tmp/texmf-home

ENV PATH="/app/.venv/bin:${PATH}"

USER 65532:65532

ENTRYPOINT ["mmdash-worker"]
