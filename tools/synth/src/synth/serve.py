"""FastAPI WebSocket mock that impersonates tasty's dxLink gateway.

Reads a DuckDB file populated by `synth.generate`, and when a client
subscribes over the dxLink protocol, replays the stored quote stream at
the original tick cadence (optionally speed-scaled). Auth is a noop —
any token the client sends is accepted.

Design notes
------------
* Per-connection state (subscribed (event, symbol) pairs, feed channel
  id, playback task) lives on a local ``Session`` dataclass — no global
  state, multiple concurrent clients are fine.
* Playback is a single asyncio task that walks the DuckDB table in
  timestamp order, sleeping until each row's scheduled wall-clock time.
  The first row's db-timestamp is anchored to whatever time the client
  subscribed, not to the stored absolute timestamp — otherwise a stale
  DB would produce a flood of past-due ticks.
* The playback task re-reads the subscription set on every row. Late
  subscribe/unsubscribe just expands/contracts what gets emitted; no
  task restart needed.
"""

from __future__ import annotations

import argparse
import asyncio
import json
import logging
import time
from dataclasses import dataclass, field
from typing import Any, Optional

import ibis
import uvicorn
from fastapi import FastAPI, WebSocket, WebSocketDisconnect

from synth import protocol as p

log = logging.getLogger("synth.serve")


@dataclass
class Session:
    ws: WebSocket
    feed_channel: Optional[int] = None
    subscribed: set[tuple[str, str]] = field(default_factory=set)
    playback_task: Optional[asyncio.Task] = None
    # Starlette's WebSocket is NOT safe for concurrent writers — two
    # `await ws.send_text(...)` calls from different tasks can interleave
    # the outgoing frames. We have exactly that shape here (the request
    # handler acks control messages while the replay task emits
    # FEED_DATA), so serialize all writes through this lock.
    send_lock: asyncio.Lock = field(default_factory=asyncio.Lock)


async def send(session: Session, msg: dict[str, Any]) -> None:
    async with session.send_lock:
        await session.ws.send_text(json.dumps(msg))


def _empty_feed_config(channel: int) -> dict[str, Any]:
    return {
        "type": p.FEED_CONFIG,
        "channel": channel,
        "aggregationPeriod": 1.0,
        "dataFormat": p.FEED_DATA_FORMAT_FULL,
        # The Go client's FeedEventFields struct has PropertyNames /
        # AdditionalProperties; emit empty values so json.Unmarshal
        # succeeds and the client doesn't warn.
        "eventFields": {"propertyNames": "", "additionalProperties": []},
    }


async def replay(
    session: Session,
    channel: int,
    db_path: str,
    speed: float,
) -> None:
    """Stream FEED_DATA from DuckDB at the stored cadence until cancelled.

    The full quote table is loaded up-front (cheap for the row counts we
    care about — a day of 100ms ticks across 10 symbols is < 10M rows)
    and then iterated row-by-row. The subscription membership check is
    done per-row against the live `session.subscribed` set, so a
    late-added symbol starts emitting as soon as the iterator reaches
    one of its rows — no task restart required.
    """
    try:
        # ibis/duckdb is synchronous, and a to_pandas() on a non-trivial
        # trajectory can easily block the event loop for hundreds of ms.
        # Run it in the default thread pool so the request handler keeps
        # responding to FEED_SUBSCRIPTION and KEEPALIVE during the load.
        def load_df():
            con = ibis.duckdb.connect(db_path)
            quotes = con.table("quotes")
            return quotes.order_by(quotes.ts_ns).to_pandas()

        df = await asyncio.to_thread(load_df)
    except Exception as exc:  # pragma: no cover — surface DB errors clearly
        log.error("replay: failed to read %s: %s", db_path, exc)
        return

    if df.empty:
        log.info("replay: empty DB")
        return

    data_start_ns = int(df.iloc[0]["ts_ns"])
    log.info(
        "replay: %d rows across %s symbols, speed=%.2fx (looping; filtering against live subscription)",
        len(df),
        sorted({str(s) for s in df["symbol"].unique()}),
        speed,
    )

    # Loop the stored trajectory forever so dev sessions aren't bounded
    # by the DB length. Each lap re-anchors `wall_start_ns` to "now" so
    # the inter-tick pacing is preserved seamlessly across the wrap.
    lap = 0
    while True:
        lap += 1
        wall_start_ns = time.time_ns()
        if lap > 1:
            log.info("replay: lap %d", lap)
        for _, row in df.iterrows():
            key = (row["event_type"], row["symbol"])
            offset_ns = int((int(row["ts_ns"]) - data_start_ns) / speed)
            target_wall_ns = wall_start_ns + offset_ns
            now_ns = time.time_ns()
            if target_wall_ns > now_ns:
                await asyncio.sleep((target_wall_ns - now_ns) / 1e9)
            # Re-check subscription membership AFTER sleeping so the latest
            # add/remove is honored.
            if key not in session.subscribed:
                continue
            payload = {
                "eventType": row["event_type"],
                "eventSymbol": row["symbol"],
                "bidPrice": float(row["bid_price"]),
                "askPrice": float(row["ask_price"]),
                "bidSize": float(row["bid_size"]),
                "askSize": float(row["ask_size"]),
            }
            try:
                await send(
                    session,
                    {
                        "type": p.FEED_DATA,
                        "channel": channel,
                        "data": [payload],
                    },
                )
            except (WebSocketDisconnect, RuntimeError):
                return


async def handle_client(ws: WebSocket, db_path: str, speed: float) -> None:
    await ws.accept()
    session = Session(ws=ws)

    try:
        while True:
            raw = await ws.receive_text()
            msg = json.loads(raw)
            mtype = msg.get("type")
            channel = int(msg.get("channel", 0))

            if mtype == p.SETUP:
                # Echo back the client's declared keepalive values — the
                # client's KeepAliveTimeout field is what it wants the
                # server to honor, and AcceptKeepAliveTimeout is what it
                # will accept from us. Mirroring both keeps parity.
                await send(
                    session,
                    {
                        "type": p.SETUP,
                        "channel": 0,
                        "keepaliveTimeout": int(msg.get("keepaliveTimeout", 60)),
                        "acceptKeepaliveTimeout": int(
                            msg.get("acceptKeepaliveTimeout", 60)
                        ),
                        "version": "0.1-synth",
                    },
                )
            elif mtype == p.AUTH:
                # Noop — any token is fine.
                await send(
                    session,
                    {
                        "type": p.AUTH_STATE,
                        "channel": 0,
                        "state": p.AUTH_STATE_AUTHORIZED,
                        "userId": "synth",
                    },
                )
            elif mtype == p.KEEPALIVE:
                await send(session, {"type": p.KEEPALIVE, "channel": 0})
            elif mtype == p.CHANNEL_REQUEST:
                # The client uses this channel for all subsequent
                # FEED_SETUP / FEED_SUBSCRIPTION messages; we just
                # acknowledge it and remember the id.
                session.feed_channel = channel
                params = msg.get("parameters", {}) or {}
                await send(
                    session,
                    {
                        "type": p.CHANNEL_OPENED,
                        "channel": channel,
                        "service": p.CHANNEL_SERVICE_FEED,
                        "parameters": {
                            "contract": params.get(
                                "contract", p.FEED_CONTRACT_STREAM
                            )
                        },
                    },
                )
            elif mtype == p.FEED_SETUP:
                await send(session, _empty_feed_config(channel))
            elif mtype == p.FEED_SUBSCRIPTION:
                adds = msg.get("add") or []
                rems = msg.get("remove") or []
                log.info(
                    "FEED_SUBSCRIPTION: add=%s remove=%s",
                    [(a.get("type"), a.get("symbol")) for a in adds],
                    [(r.get("type"), r.get("symbol")) for r in rems],
                )
                for add in adds:
                    session.subscribed.add((add["type"], add["symbol"]))
                for rem in rems:
                    session.subscribed.discard((rem["type"], rem["symbol"]))
                # No FEED_CONFIG ack — real tastytrade treats FEED_SUBSCRIPTION
                # as fire-and-forget, and an earlier version of this mock sent
                # one anyway, which hid a 30s-timeout bug in the Go client for
                # months. Playback starts unconditionally on the first
                # subscription so the first FEED_DATA frame is the only
                # implicit ack the client gets (same as real tastytrade).
                if (
                    session.feed_channel is not None
                    and (
                        session.playback_task is None
                        or session.playback_task.done()
                    )
                ):
                    session.playback_task = asyncio.create_task(
                        replay(session, session.feed_channel, db_path, speed)
                    )
            elif mtype == p.CHANNEL_CANCEL:
                await send(
                    session,
                    {"type": p.CHANNEL_CLOSED, "channel": channel},
                )
                if session.playback_task:
                    session.playback_task.cancel()
                session.subscribed.clear()
                session.feed_channel = None
            else:
                log.debug("unhandled message type=%s payload=%s", mtype, msg)
    except WebSocketDisconnect:
        pass
    except Exception as exc:
        log.exception("session error: %s", exc)
    finally:
        if session.playback_task:
            session.playback_task.cancel()
        try:
            await ws.close()
        except Exception:
            pass


def make_app(db_path: str, speed: float) -> FastAPI:
    app = FastAPI(title="godxfeed-synth")

    # The real gateway is served at wss://.../realtime — matching the
    # path keeps clients agnostic about which backend they're dialing.
    @app.websocket("/realtime")
    async def realtime(ws: WebSocket) -> None:
        await handle_client(ws, db_path, speed)

    # Alias at "/ws" for test harnesses that prefer a shorter path.
    @app.websocket("/ws")
    async def ws_alt(ws: WebSocket) -> None:
        await handle_client(ws, db_path, speed)

    @app.get("/healthz")
    async def healthz() -> dict[str, str]:
        return {"status": "ok", "db": db_path}

    return app


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--db", required=True, help="Path to DuckDB database built by synth-generate.")
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int, default=9999)
    ap.add_argument(
        "--speed",
        type=float,
        default=1.0,
        help="Playback speed multiplier (2.0 = twice as fast, 0.5 = half).",
    )
    ap.add_argument("--log-level", default="info")
    return ap.parse_args(argv)


def main() -> int:
    args = parse_args()
    logging.basicConfig(
        level=args.log_level.upper(),
        format="%(asctime)s %(name)s %(levelname)s %(message)s",
    )
    uvicorn.run(
        make_app(args.db, args.speed),
        host=args.host,
        port=args.port,
        log_level=args.log_level,
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
