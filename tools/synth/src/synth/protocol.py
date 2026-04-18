"""dxLink protocol message-type constants.

Mirrors the constants in `dxclient/message.go` so the mock is a drop-in
replacement for tasty's dxFeed gateway. Kept in its own module so both
the server and any tests can import it without circular dependencies.
"""

from __future__ import annotations

SETUP = "SETUP"
KEEPALIVE = "KEEPALIVE"
AUTH = "AUTH"
AUTH_STATE = "AUTH_STATE"
CHANNEL_REQUEST = "CHANNEL_REQUEST"
CHANNEL_OPENED = "CHANNEL_OPENED"
CHANNEL_CANCEL = "CHANNEL_CANCEL"
CHANNEL_CLOSED = "CHANNEL_CLOSED"
FEED_SETUP = "FEED_SETUP"
FEED_CONFIG = "FEED_CONFIG"
FEED_SUBSCRIPTION = "FEED_SUBSCRIPTION"
FEED_DATA = "FEED_DATA"
ERROR = "ERROR"

AUTH_STATE_AUTHORIZED = "AUTHORIZED"
AUTH_STATE_UNAUTHORIZED = "UNAUTHORIZED"

CHANNEL_SERVICE_FEED = "FEED"

FEED_CONTRACT_STREAM = "STREAM"
FEED_DATA_FORMAT_FULL = "FULL"
