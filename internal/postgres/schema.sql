CREATE TABLE IF NOT EXISTS engine_events (
    sequence BIGINT NOT NULL,
    event_index INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    symbol TEXT,
    order_id TEXT,
    side TEXT,
    order_type TEXT,
    price BIGINT,
    quantity BIGINT,
    maker_order_id TEXT,
    taker_order_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (sequence, event_index)
);

CREATE INDEX IF NOT EXISTS engine_events_symbol_sequence_idx
    ON engine_events (symbol, sequence);

CREATE TABLE IF NOT EXISTS orders (
    id TEXT PRIMARY KEY,
    symbol TEXT NOT NULL,
    side TEXT NOT NULL,
    order_type TEXT NOT NULL,
    price BIGINT,
    original_quantity BIGINT NOT NULL,
    remaining_quantity BIGINT NOT NULL,
    status TEXT NOT NULL,
    accepted_sequence BIGINT NOT NULL,
    updated_sequence BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS orders_symbol_status_idx
    ON orders (symbol, status);

CREATE TABLE IF NOT EXISTS trades (
    sequence BIGINT NOT NULL,
    trade_index INTEGER NOT NULL,
    symbol TEXT NOT NULL,
    maker_order_id TEXT NOT NULL,
    taker_order_id TEXT NOT NULL,
    price BIGINT NOT NULL,
    quantity BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (sequence, trade_index)
);

CREATE INDEX IF NOT EXISTS trades_symbol_sequence_idx
    ON trades (symbol, sequence);

CREATE TABLE IF NOT EXISTS accounts (
    id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS positions (
    account_id TEXT NOT NULL REFERENCES accounts(id),
    symbol TEXT NOT NULL,
    quantity BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, symbol)
);
