# syntax=docker/dockerfile:1
FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    UV_LINK_MODE=copy

RUN pip install --no-cache-dir uv==0.5.25

WORKDIR /app

COPY pyproject.toml uv.lock ./
COPY src/ ./src/

RUN uv sync --frozen --no-dev

ENV ANALYTICS_HOST=0.0.0.0 \
    ANALYTICS_PORT=8090
EXPOSE 8090

CMD ["uv", "run", "--no-dev", "analytics-serve"]
