# godxlink

There's no Go client library for `dxlink` so I'm building this one.

Here's the reference repo: https://github.com/dxFeed/dxLink

## How to Use

For most operations, you'll need a session token, and for streaming DXLink data you'll also need a streamer token. These expire after 24 hours; don't request too many of these or you'll risk getting blocked from the API. By default, the CLI will look for these under the `SESSION_TOKEN` and `STREAMER_TOKEN` envs, so set those however you'd like.

First get a session token:

```bash
./cli get-session-token -u TW_USERNAME -p TW_PWD
```

Store that under `SESSION_TOKEN` and then get a streamer token and store _that_ under `STREAMER_TOKEN`:

```bash
./cli get-streamer-token [--session-token=SESSION_TOKEN]
```

The main use case for this package is running an HTTP server that has an associated DXLink client. The client receives quote data, and the server forwards it to subscribed clients. The server has some other interesting routes that clients can subscribe to, including a real time stream of theoretical option prices, as well as time series for historical price data of a given symbol. This is especially useful for seeing how the price of a particular option symbol has evolved over time.
