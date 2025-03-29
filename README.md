# godxlink

FIXME: DO NOT COMMIT, I NEED TO PURGE THE MAKEFILE HISTORY BECAUSE I THINK I CHECKED IN MY CREDENTIALS

There's no Go client library for `dxfeed/dxlink` so I'm building this one.

Here's the reference [repo](https://github.com/dxFeed/dxLink).

This also assumes that you have a TastyTrade developer account setup. Check out the docs [here](https://developer.tastytrade.com/).

The main use case for this package is running an HTTP server that has an associated DXLink client. The DXLink client (i.e., in `dxclient`) runs as part of the server process and receives quote data. The server forwards it to subscribed browser clients over NATS. Clients can subscribe to the NATS stream and receive symbol data. Browser clients will typically plot this data in some way.

Additionally, the server is also responsible for writing the timeseries ticker data for a predefined set of symbols to TimescaleDB. This way, we can provide time series data for a given symbol over the course of it's existence. It will be interesting to see how the price of a contract evolves over time, and how it compares to the theoretical price of the contract. We'll capture both the bid and ask prices, and since the VIX is one of the symbols, we'll also be able to see how the "theoretical" price evolves over time.

## How To: HTTP Server

Open a new terminal, and do:

```bash
make && make run-http-server
```

Ok, now you should have an HTTP server listening on `:8080`. For typical usage, you're good to go. However, you can also run a "minimal" version of the server that doesn't connect to any external entities (except the DB) with the `--minimal-setup` flag.

Important flags:

- `--listen-port`: Port to listen on (default: 8080)
- `--minimal-setup`: Run server with minimal dependencies
- `--log-level`: Set logging level (default: info)
- `--max-symbol-count`: Maximum number of symbols to track
- `--symbol-method-n-related`: Use related symbols method for symbol selection
- `--handler-debug`: Enable debug logging for handlers
- `--handler-persist`: Enable data persistence to database
- `--handler-publish`: Enable publishing to NATS

## How To: CLI

### TL;DR: You have to set up the env every day (due to TastyTrade token expirations), and you can to that easily:

```bash
# First ensure the HTTP server is running (in another terminal)
make && make-run-http-minimal

# In a second terminal
make refresh-env
```

IMPORTANT: remember if you're running commands from the CLI (like running the HTTP server "by hand" and not via the `Makefile`), then you'll want to set all the relevant environment variables in your shell's env.

```bash
export $(grep -v '^#' service/.env | xargs)
```

or you can do

```bash
set -o allexport && source service/.env && set +o allexport
```

### Details, details...

For most operations, you'll need a session token from TastyTrade, and for streaming DXLink data you'll also need a streamer token. These expire after 24 hours. Don't request too many of these or you'll risk getting blocked from the API. By default, the CLI will look for these under the `SESSION_TOKEN` and `STREAMER_TOKEN` envs, so set those however you'd like.

First get a session token (this is the only time you need to type in sensitive information):

```bash
./cli admin get-session-token --env-file service/.env
```

This should automatically store the session token under `SESSION_TOKEN` in your .env file. Then get a streamer token and store _that_ under `STREAMER_TOKEN`:

```bash
./cli get-streamer-token --env-file service/.env
```

You'll then likely need to get a Bearer token. You need to run the HTTP server for this, but you can do (assuming you're running at least a minimal HTTP server):

```bash
./cli admin get-bearer-token --env-file service/.env
```

Remember, you can skip all of this with

```bash
make refresh-env
set -o allexport && source service/.env && set +o allexport
```

Get in the habit of doing this since this file has a number of other envs you need to specify (e.g., connection strings, ports, keys, etc.).

## Web UI

Open a browser at `http://localhost:8080`. You should be prompted for an authorization token. You can use the `BEARER_TOKEN` env. It will store this in your localStorage for subsequent authorization.

### Available Plots

The server provides several different plot types accessible through the browser:

1. **Options Grid** (`/plots?plot_kind=options_grid&symbol=SYMBOL`)

   - Displays options chain data in a grid format
   - Shows calls and puts with their respective bid/ask prices
   - Real-time updates available

2. **Line Chart** (`/plots?plot_kind=line_chart&symbol=SYMBOL`)

   - Tracks price movements over time
   - Interactive time series visualization
   - Supports auto-updating

3. **Ridgeline Plot** (`/plots?plot_kind=ridgeline&symbol=SYMBOL`)

   - Visualizes option price distributions across different strikes
   - Shows density curves for price distributions
   - Useful for analyzing price clustering

4. **Dynamic Distribution** (`/plots?plot_kind=dynamic_distribution&symbol=SYMBOL`)
   - Real-time price distribution changes
   - Updates automatically as new data arrives
   - Useful for monitoring price movement patterns

## NATS Connectivity

Browser clients connect to the NATS server using WebSocket transport. Here's how it works:

1. **Connection Setup**:

   ```javascript
   const nc = await connect({
     servers: NATS_URL,
     token: await getNatsToken(), // Gets token from localStorage
   });
   ```

2. **Subscribe to Updates**:

   ```javascript
   const sub = nc.subscribe("symbol.updates");
   for await (const m of sub) {
     const data = JSON.parse(m.data);
     // Handle the update
   }
   ```

3. **Authentication**:

   - Uses auth callout endpoint `/nats-auth-callout`
   - Server validates bearer token and issues NATS token
   - Client just needs to supply their regular Bearer JWT.

4. **Data Format**:
   - Updates are sent as JSON messages
   - Include symbol, price, and timestamp information
   - Can be filtered by symbol on the client side

The NATS browser URL is configurable through the `NATS_BROWSER_URL` environment variable.

## Payment/Subscriptions

All auth is handled via JWT. How do clients get that JWT to begin with? We send it them in an email after they pay us. It expires in XX days (depending on how much they paid us.) How do we know they've paid us? We have a webhook for BuyMeACoffee payments! We extract their email and payment amount from the payload and send them an email with the API token. If they lose it, too bad (we'll be clear about that in the email).

Here's some important details about the webhook handler:

1. **Endpoint**: `/webhook/buy-me-a-coffee`

   - Accepts POST requests
   - Expects JSON payload with email, amount, and message
   - No authentication required (uses BuyMeACoffee's webhook signature for validation)

2. **Payment Tiers**:

   - Basic Access (1 coffee): 30-day access
   - Premium Access (3 coffees): 90-day access
   - Enterprise Access (5+ coffees): 180-day access

3. **Process Flow**:

   - Webhook receives payment notification
   - Validates webhook signature
   - Generates JWT based on payment amount
   - Sends email with JWT and usage instructions
   - Stores transaction details for audit purposes

4. **Security Notes**:

   - JWTs cannot be refreshed or extended
   - One JWT per payment transaction
   - Email delivery is best-effort (no resends)

5. **API Access**:
   - JWT must be included in Authorization header
   - Format: `Authorization: Bearer <token>`
   - Token expiry is based on payment tier
   - No trial or free tier available (unless I send you a token manually).

## Setting up the Development Environment

Here's the external services you need to run:

1. **Database (TimescaleDB)**:

   - Install TimescaleDB (PostgreSQL extension)
   - Create database and configure connection string in `.env`
   - Required for storing time series ticker data

2. **Ngrok Proxy**:

   - Install Ngrok for webhook testing
   - Use the Ngrok URL as your webhook endpoint for testing
   - Set up a fixed domain in your Ngrok account
   - Run: `ngrok http 8080` or with a fixed domain:
     ```bash
     ngrok http --domain your-domain.ngrok.app 8080
     ```
   - Ngrok domain env can be used like:
     ```bash
     ngrok http --domain ${NGROK_DOMAIN} 8080
     ```

3. **NATS Server**:

   - Install and run NATS server
   - Configure `NATS_BROWSER_URL` and other NATS-related env variables
   - Required for real-time data streaming to browser clients
   - You can run the nats server with:

   ```bash
   nats-server -c service/nats.conf
   ```

4. **NATS Dummy Publisher**:

   - Useful for testing and development without live market data
   - Publishes simulated market data to NATS
   - Run with:
     ```bash
     make make-nats-dummy-publisher
     ```
   - Generates random quotes for SPY every 150ms
   - Data format matches production:
     ```javascript
     {
       "eventSymbol": "SPY",
       "bidPrice": 100.00,
       "askPrice": 100.10,
       "bidSize": 100,
       "askSize": 100,
       "eventType": "quote"
     }
     ```
   - Perfect for:
     - Testing plot functionality
     - UI development
     - Integration testing
     - Local development without market access

5. **TastyTrade Account**:

   - Sign up for a TastyTrade developer account
   - Get your API credentials
   - Set up `TW_USERNAME` and `TW_PASSWORD` in your environment

6. **Development Tools**:

   - Go development environment
   - Make utility (for running Makefile commands)
   - Git for version control

7. **Environment Variables**:

   - Copy `service/.env.example` to `service/.env` (if available)
   - Configure all required environment variables
   - Use `export $(grep -v '^#' service/.env | xargs)` to load them

8. **Build and Run**:
   - Run `make` to build and start the HTTP server
   - Server will be available on port 8080 by default
   - Use `--minimal-setup` flag for testing without external dependencies

## Authentication and Navigation

The web UI uses a token-based authentication system with the following flow:

1. **Initial Authentication**:

   - User is prompted for API token on first visit
   - Token is stored in browser's localStorage
   - All API requests include token in Authorization header

2. **Page Navigation**:

   - Token is temporarily passed as URL parameter during navigation
   - Receiving page immediately:
     - Extracts token from URL
     - Stores it in localStorage
     - Removes token from URL
   - This maintains authentication state across page navigation
   - Example flow:

     ```javascript
     // User clicks link to plot
     /plots?plot_kind=line_chart&symbol=SPY&token=xxx

     // Page loads and immediately cleans URL to
     /plots?plot_kind=line_chart&symbol=SPY
     ```

3. **Security Notes**:

   - Token is only briefly exposed in URL during navigation
   - Token is immediately removed from URL to prevent accidental sharing
   - All subsequent API requests use proper Authorization headers
   - No tokens are stored in browser history

4. **Session Management**:
   - Logout clears token from localStorage
   - Invalid tokens trigger re-authentication
   - Token verification occurs on every page load
   - Failed authentication redirects to login modal
