package postgres

const insertEventSQL = `
INSERT INTO engine_events (
    sequence,
    event_index,
    event_type,
    symbol,
    order_id,
    side,
    order_type,
    price,
    quantity,
    maker_order_id,
    taker_order_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (sequence, event_index) DO NOTHING`

const upsertAcceptedOrderSQL = `
INSERT INTO orders (
    id,
    symbol,
    side,
    order_type,
    price,
    original_quantity,
    remaining_quantity,
    status,
    accepted_sequence,
    updated_sequence
) VALUES ($1, $2, $3, $4, $5, $6, $7, 'accepted', $8, $9)
ON CONFLICT (id) DO NOTHING`

const updateOrderRestedSQL = `
UPDATE orders
SET remaining_quantity = $1,
    status = 'resting',
    updated_sequence = $2,
    updated_at = now()
WHERE id = $3`

const insertTradeSQL = `
INSERT INTO trades (
    sequence,
    trade_index,
    symbol,
    maker_order_id,
    taker_order_id,
    price,
    quantity
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (sequence, trade_index) DO NOTHING`

const reduceMakerOrderSQL = `
UPDATE orders
SET remaining_quantity = GREATEST(remaining_quantity - $1, 0),
    status = CASE
        WHEN GREATEST(remaining_quantity - $1, 0) = 0 THEN 'filled'
        ELSE 'partially_filled'
    END,
    updated_sequence = $2,
    updated_at = now()
WHERE id = $3`

const reduceTakerOrderSQL = `
UPDATE orders
SET remaining_quantity = GREATEST(remaining_quantity - $1, 0),
    status = CASE
        WHEN GREATEST(remaining_quantity - $1, 0) = 0 THEN 'filled'
        ELSE 'partially_filled'
    END,
    updated_sequence = $2,
    updated_at = now()
WHERE id = $3`

const cancelOrderSQL = `
UPDATE orders
SET status = 'canceled',
    remaining_quantity = 0,
    updated_sequence = $1,
    updated_at = now()
WHERE id = $2`
