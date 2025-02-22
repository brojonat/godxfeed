# godxlink

There's no Go client library for `dxfeed/dxlink` so I'm building this one.

Here's the reference [repo](https://github.com/dxFeed/dxLink).

This also assumes that you have a TastyTrade developer account setup. Check out the docs [here](https://developer.tastytrade.com/).

The main use case for this package is running an HTTP server that has an associated DXLink client. The DXLink client (i.e., in `dxclient`) runs as part of the server process and receives quote data. The server forwards it to subscribed browser clients over NATS. Clients can subscribe to the NATS stream and receive symbol data. Browser clients will typically plot this data in some way.

Additionally, the server is also responsible for writing the timeseries ticker data for a predefined set of symbols to TimescaleDB. This way, we can provide time series data for a given symbol over the course of it's existence. It will be interesting to see how the price of a contract evolves over time, and how it compares to the theoretical price of the contract. We'll capture both the bid and ask prices, and since the VIX is one of the symbols, we'll also be able to see how the "theoretical" price evolves over time.

## How to Use

For most operations, you'll need a session token from TastyTrade, and for streaming DXLink data you'll also need a streamer token. These expire after 24 hours. Don't request too many of these or you'll risk getting blocked from the API. By default, the CLI will look for these under the `SESSION_TOKEN` and `STREAMER_TOKEN` envs, so set those however you'd like.

First get a session token:

```bash
./cli get-session-token -u {{TW_USERNAME}} -p {{TW_PASSWORD}}
```

Store the session token under `SESSION_TOKEN` in your env and then get a streamer token and store _that_ under `STREAMER_TOKEN`:

```bash
./cli get-streamer-token --session-token {{SESSION_TOKEN}}
```

While you're at it, you should set these in the `service/.env` file as well. You can export all the env vars in that file with:

```bash
export $(grep -v '^#' service/.env | xargs)
```

Get in the habit of doing this since this file has a number of other envs you need to specify (e.g., database and Temporal connection strings, ports, keys, etc.).

Assuming you've run the above, you can do a smoke test to get your account info with the following:

```bash
# in one terminal, run the HTTP server on :8080
go build -o cli cmd/godxfeed/*.go && ./cli run http-server
# in another terminal, first you need to get a JWT for interacting with the server
curl -X POST -u "{{EMAIL}}:{{SERVER_SECRET_KEY}}" http://localhost:8080/token
# now you can test your session token
curl -H "Authorization: Bearer {{AUTH_TOKEN}}" http://localhost:8080/test-session-token --url-query session-token={{SESSION_TOKEN}}
```

Ok, cool, hopefully you're connected to your account. Now we can get some symbol data:

```bash
./cli data symbols --symbol SPY
```

And to get the options chain for a symbol, you can do the following (note that we're using `jq` to manipulate the rich output and get to the "relevant" data under `data.items`). Anyway, here's an example command:

```
./cli data option-chain -s SPY | jq '.data.items | .[0:500] | map({"streamer-symbol": .["streamer-symbol"], DTE: .["days-to-expiration"]})'
```
