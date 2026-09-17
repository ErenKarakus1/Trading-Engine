# Trading Engine

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?style=for-the-badge&logo=go&logoColor=white)
![Gin](https://img.shields.io/badge/Gin-Framework-00ADD8?style=for-the-badge&logo=gin&logoColor=white)
![React](https://img.shields.io/badge/React-Frontend-61DAFB?style=for-the-badge&logo=react&logoColor=black)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-Database-4169E1?style=for-the-badge&logo=postgresql&logoColor=white)
![Redis](https://img.shields.io/badge/Redis-Rate%20Limiting-DC382D?style=for-the-badge&logo=redis&logoColor=white)
![Kafka](https://img.shields.io/badge/Kafka-Event%20Streaming-231F20?style=for-the-badge&logo=apachekafka&logoColor=white)
![Prometheus](https://img.shields.io/badge/Prometheus-Metrics-E6522C?style=for-the-badge&logo=prometheus&logoColor=white)

A Go trading engine with deterministic limit-order matching, risk checks, event persistence, Kafka publishing, Redis rate limiting, Binance market-data sync, Prometheus metrics, and a React dashboard for order entry and book monitoring.

> **Portfolio / learning project:** This project simulates an exchange-style matching engine. It is not a production trading venue, broker, investment system, or financial service. Binance data is used as external market reference data only; orders submitted through the dashboard are matched by the internal engine.

## Table of Contents

* [Features](#features)
* [Tech Stack](#tech-stack)
* [Architecture](#architecture)
* [Services](#services)
* [Project Structure](#project-structure)
* [Matching Engine](#matching-engine)
* [Market Data](#market-data)
* [Risk & Rate Limiting](#risk--rate-limiting)
* [Persistence & Events](#persistence--events)
* [API Endpoints](#api-endpoints)
* [Data Model](#data-model)
* [Dashboard](#dashboard)
* [Setup](#setup)
* [Observability](#observability)
* [Known Limitations](#known-limitations)
* [Future Improvements](#future-improvements)
* [License](#license)

## Features

* Deterministic price-time-priority matching engine
* Limit and market order support
* Order cancellation
* Internal order book and trade tape
* Separate Binance market-data book
* Internal/Binance book switching in the dashboard
* Startup seed orders so the internal dashboard is not empty
* Risk checks for balances, positions, order size, and price collars
* Redis-backed Gin rate limiting
* PostgreSQL event, order, trade, position, and snapshot storage
* Kafka publishing for matching events
* Startup restore from snapshots, replayed events, and persisted trades
* Execution/slippage simulator using market-data snapshots
* WebSocket updates for internal order-book events
* WebSocket updates for external market data
* Prometheus metrics endpoint
* Docker Compose stack for API, frontend, Postgres, Redis, Kafka, Prometheus, and Grafana

## Tech Stack

* **Language:** Go 1.26
* **Web Framework:** [Gin](https://github.com/gin-gonic/gin)
* **Frontend:** React + Vite
* **Database:** PostgreSQL via [pgx/v5](https://github.com/jackc/pgx)
* **Cache / Rate Limiting:** Redis via [go-redis/v9](https://github.com/redis/go-redis)
* **Messaging:** Kafka via [segmentio/kafka-go](https://github.com/segmentio/kafka-go)
* **WebSockets:** [gorilla/websocket](https://github.com/gorilla/websocket)
* **Market Data:** Binance Spot depth WebSocket adapter
* **Observability:** Prometheus metrics + Grafana provisioning
* **Architecture:** Modular monolith backend + separate React dashboard + event-driven integrations

## Architecture

```mermaid
flowchart LR
    Browser[React Dashboard]

    subgraph API["trading-engine API :8080"]
        Gin[Gin REST API]
        WS[WebSocket endpoints]
        Match[Matching Engine]
        Risk[Risk Engine]
        MD[Market Data Book]
        Metrics[Prometheus Metrics]
    end

    Binance[Binance WebSocket]
    Redis[(Redis)]
    Postgres[(PostgreSQL)]
    Kafka[/Kafka/]
    Prometheus[Prometheus]
    Grafana[Grafana]

    Browser --> Gin
    Browser <--> WS

    Gin --> Risk
    Risk --> Match
    Gin <--> Redis
    Match --> Postgres
    Match --> Kafka
    Binance --> MD
    MD --> WS

    Prometheus --> Metrics
    Grafana --> Prometheus
```

The internal matching book and the Binance market-data book are deliberately separate.

```text
Internal book = orders submitted to this engine
Binance book  = external market reference data
```

The dashboard can switch between them, but Binance data is not inserted into the internal matching engine.

## Services

| Service      | Port | Responsibility                                      |
| ------------ | ---: | --------------------------------------------------- |
| `api`        | 8080 | REST API, WebSockets, matching, risk, metrics       |
| `web`        | 5173 | React dashboard served by Nginx in Docker Compose   |
| `postgres`   | 5432 | Event, order, trade, snapshot, account persistence  |
| `redis`      | 6379 | Request rate limiting                               |
| `kafka`      | 9092 | Matching-event stream                               |
| `prometheus` | 9090 | Metrics scraping                                    |
| `grafana`    | 3000 | Metrics dashboarding                                |

## Project Structure

```text
Trading-Engine/
|-- cmd/
|   `-- trading-engine/
|       `-- main.go
|
|-- internal/
|   |-- api/             # Gin handlers, REST routes, WebSockets
|   |-- app/             # Application wiring and startup
|   |-- domain/          # Shared domain types
|   |-- execution/       # Market-order execution/slippage simulator
|   |-- kafka/           # Kafka event publisher
|   |-- marketdata/      # External market-data book, syncer, Binance feed
|   |-- matching/        # Deterministic matching engine, replay, snapshots
|   |-- observability/   # Prometheus metrics
|   |-- orderbook/       # Price-level order book
|   |-- postgres/        # pgx store and schema
|   |-- ratelimit/       # Redis rate limiter
|   `-- risk/            # Risk checks
|
|-- web/
|   |-- src/
|   |   |-- main.jsx
|   |   `-- styles.css
|   |-- Dockerfile
|   |-- nginx.conf
|   |-- package.json
|   `-- vite.config.js
|
|-- deploy/
|   |-- prometheus/
|   `-- grafana/
|
|-- docker-compose.yml
|-- Dockerfile
|-- go.mod
`-- README.md
```

## Matching Engine

The matching engine owns the internal order book.

Supported order types:

```text
limit
market
```

Supported sides:

```text
buy
sell
```

Matching follows price-time priority:

* Buy orders match against the lowest sell price.
* Sell orders match against the highest buy price.
* Resting orders remain in the internal book until matched or canceled.
* Events are assigned deterministic engine sequence numbers.

Startup seed orders currently populate `BTC-USD` so the dashboard has initial internal-book data.

## Market Data

Binance market data is consumed separately from the internal matching book.

Default configured symbols:

```text
BTC-USDT
ETH-USDT
```

The frontend maps internal symbols to Binance symbols:

```text
BTC-USD -> BTC-USDT
ETH-USD -> ETH-USDT
```

Binance price and quantity values are stored internally as scaled integers:

```text
price scale:    100
quantity scale: 100000000
```

The dashboard displays these as human-readable decimal values.

## Risk & Rate Limiting

The risk engine validates:

* Maximum order quantity
* Maximum position
* Buy-side cash balance
* Sell-side available position
* Optional price collars

Redis-backed rate limiting is applied through Gin middleware-style integration when Redis is available. If Redis is unavailable, the app logs the failure and continues without rate limiting.

## Persistence & Events

PostgreSQL persistence includes:

* Matching events
* Orders
* Trades
* Engine snapshots
* Accounts
* Positions

Kafka is currently publisher-only in this repository. The API publishes matching events to the `trading-engine-events` topic for downstream services such as audit, analytics, settlement, or notifications. No Kafka consumer is implemented in this repo; PostgreSQL remains the source for recovery.

PostgreSQL event and snapshot persistence completes before an order response is returned, so restart recovery can restore the order book, trade tape, and recent event history. Kafka publishing is asynchronous and is not used as the recovery source.

On startup, the API restores the configured internal symbol from:

* The latest engine snapshot
* Matching events after that snapshot
* Persisted trades for the trade tape

## API Endpoints

Base URL:

```text
http://localhost:8080
```

### Orders

| Method | Path          | Description            |
| ------ | ------------- | ---------------------- |
| POST   | `/orders`     | Submit an order        |
| GET    | `/orders/:id` | Get recorded order     |
| DELETE | `/orders/:id` | Cancel a resting order |

**POST `/orders`**

```json
{
  "account_id": "demo",
  "id": "buy-1",
  "symbol": "BTC-USD",
  "side": "buy",
  "type": "limit",
  "price": 100,
  "quantity": 1
}
```

### Internal Book & Trades

| Method | Path                 | Description                  |
| ------ | -------------------- | ---------------------------- |
| GET    | `/orderbook/:symbol` | Internal best bid / best ask |
| GET    | `/trades/:symbol`    | Internal trade tape          |
| GET    | `/events/:symbol`    | Recent internal event log    |

Example:

```text
GET /events/BTC-USD?limit=120
```

### Market Data

| Method | Path                  | Description                  |
| ------ | --------------------- | ---------------------------- |
| GET    | `/marketdata/:symbol` | External market-data snapshot |

Example:

```text
GET /marketdata/BTC-USDT
```

### WebSockets

| Path                       | Description                    |
| -------------------------- | ------------------------------ |
| `/ws/orderbook/:symbol`    | Internal order-book events     |
| `/ws/marketdata/:symbol`   | External market-data snapshots |

### Metrics

| Method | Path       | Description        |
| ------ | ---------- | ------------------ |
| GET    | `/metrics` | Prometheus metrics |

## Data Model

```mermaid
erDiagram
    ENGINE_EVENTS {
        bigint sequence
        int event_index
        text event_type
        text symbol
        text order_id
        text side
        text order_type
        bigint price
        bigint quantity
        text maker_order_id
        text taker_order_id
    }

    ORDERS {
        text id PK
        text symbol
        text side
        text order_type
        bigint price
        bigint original_quantity
        bigint remaining_quantity
        text status
        bigint accepted_sequence
        bigint updated_sequence
        timestamptz created_at
        timestamptz updated_at
    }

    TRADES {
        bigint sequence
        int trade_index
        text symbol
        text maker_order_id
        text taker_order_id
        bigint price
        bigint quantity
    }

    ENGINE_SNAPSHOTS {
        text symbol
        bigint sequence
        jsonb orders
        timestamptz created_at
    }

    ACCOUNTS {
        text id PK
        timestamptz created_at
    }

    POSITIONS {
        text account_id
        text symbol
        bigint quantity
    }

    ORDERS ||--o{ TRADES : maker_or_taker
    ENGINE_EVENTS ||--o{ TRADES : records
    ACCOUNTS ||--o{ POSITIONS : has
```

## Dashboard

The React dashboard supports:

* Market selection
* Internal/Binance book switching
* Internal order entry
* Trade tape
* Event log rehydrated from REST on load
* Session stats
* Live WebSocket updates

In **Internal** mode, the dashboard shows order entry, internal trades, internal volume, and matching events. On page load, it fetches the latest order book, trade tape, and recent events, then appends live WebSocket events.

In **Binance** mode, the dashboard is read-only and shows external market depth. Orders are not sent to Binance.

## Setup

### Prerequisites

* Go 1.26+
* Node.js
* Docker
* Docker Compose

### Clone

```bash
git clone https://github.com/ErenKarakus1/Trading-Engine.git
cd Trading-Engine
```

### Run with Docker Compose

```bash
docker compose up --build
```

Expected local addresses:

```text
API:        http://localhost:8080
Dashboard:  http://localhost:5173
Prometheus: http://localhost:9090
Grafana:    http://localhost:3000
```

Grafana credentials:

```text
username: admin
password: admin
```

### Run Backend Locally

```bash
go mod download
go run ./cmd/trading-engine
```

The API runs on:

```text
http://localhost:8080
```

Optional environment variables:

```text
TRADING_ENGINE_ADDR=:8080
POSTGRES_DSN=postgres://trading_engine:trading_engine@localhost:5432/trading_engine?sslmode=disable
REDIS_ADDR=localhost:6379
KAFKA_BROKERS=localhost:9092
KAFKA_TOPIC=trading-engine-events
BINANCE_DOMAIN_SYMBOLS=BTC-USDT,ETH-USDT
```

### Environment File

A sample environment file is provided:

```bash
cp .env.example .env
```

Docker Compose already includes working service defaults in `docker-compose.yml`, so copying `.env.example` is mainly useful for local non-Docker runs.

### Run Frontend Locally

```bash
cd web
npm install
npm run dev
```

The Vite dev server runs on:

```text
http://127.0.0.1:5173
```

### Tests

```bash
go test ./...
```

Frontend build:

```bash
cd web
npm run build
```

## Observability

The API exposes Prometheus metrics at:

```text
GET /metrics
```

Metrics include:

* HTTP request count and duration
* Accepted and rejected orders
* Executed trades
* Matching latency
* WebSocket connections and messages
* Market-data messages and reconnects
* Kafka consumer lag gauge placeholder for future consumers

Prometheus and Grafana are included in Docker Compose.

## Known Limitations

* This is a simulation and not a production exchange.
* There is no real authentication or user management.
* Demo account balances and positions are seeded in code.
* Kafka publishing is not implemented as a transactional outbox.
* Binance market data uses WebSocket depth snapshots and does not place orders on Binance.
* Binance connectivity depends on local network access.
* Market-data symbol support is configured at startup.
* PostgreSQL, Redis, and Kafka failures are logged and may disable related features.
* Startup restore is currently centered on the configured snapshot symbol.
* The dashboard is an operator UI, not a broker-grade trading frontend.

## Future Improvements

* Authentication and account management
* Admin tools for accounts, balances, and positions
* Transactional outbox for reliable Kafka publishing
* Historical replay UI
* Better multi-symbol market-data management
* More complete Binance REST snapshot bootstrapping
* Strategy simulation against external market data
* Order history and open-order views
* More Grafana dashboards
* Structured logging
* Integration tests with Docker containers

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
