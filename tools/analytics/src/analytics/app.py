"""FastAPI wrapper around the periodic publisher.

The service is primarily a background task, but we host it inside a
FastAPI app so it gets a /healthz endpoint, structured lifespan
management (clean NATS drain on shutdown), and slots naturally into a
k8s-style liveness/readiness probe later.
"""

from __future__ import annotations

import asyncio
import logging
import os
from contextlib import asynccontextmanager

import nats
import uvicorn
from fastapi import FastAPI

from analytics.config import Config
from analytics.publisher import publish_loop

log = logging.getLogger("analytics.app")


@asynccontextmanager
async def lifespan(app: FastAPI):
    cfg: Config = app.state.config
    connect_kwargs: dict = {"servers": [cfg.nats_url]}
    if cfg.nats_user and cfg.nats_password:
        connect_kwargs["user"] = cfg.nats_user
        connect_kwargs["password"] = cfg.nats_password
    log.info(
        "connecting to NATS %s (user=%s)",
        cfg.nats_url,
        cfg.nats_user or "<anon>",
    )
    nc = await nats.connect(**connect_kwargs)
    stop = asyncio.Event()
    task = asyncio.create_task(
        publish_loop(nc, cfg.symbols, cfg.interval_s, stop),
        name="analytics-publish-loop",
    )
    app.state.nc = nc
    app.state.stop = stop
    app.state.task = task
    try:
        yield
    finally:
        log.info("shutting down publisher")
        stop.set()
        try:
            await asyncio.wait_for(task, timeout=5.0)
        except asyncio.TimeoutError:
            log.warning("publish loop didn't exit in 5s; cancelling")
            task.cancel()
        await nc.drain()


def make_app(cfg: Config | None = None) -> FastAPI:
    cfg = cfg or Config.from_env()
    app = FastAPI(title="godxfeed-analytics", lifespan=lifespan)
    app.state.config = cfg

    @app.get("/healthz")
    async def healthz() -> dict[str, str]:
        return {"status": "ok"}

    @app.get("/config")
    async def config() -> dict:
        return {
            "nats_url": cfg.nats_url,
            "symbols": list(cfg.symbols),
            "interval_s": cfg.interval_s,
        }

    return app


def main() -> int:
    logging.basicConfig(
        level=os.environ.get("LOG_LEVEL", "INFO").upper(),
        format="%(asctime)s %(name)s %(levelname)s %(message)s",
    )
    cfg = Config.from_env()
    uvicorn.run(make_app(cfg), host=cfg.host, port=cfg.port, log_level="info")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
