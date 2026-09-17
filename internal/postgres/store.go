package postgres

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
	"github.com/ErenKarakus1/Trading-Engine/internal/matching"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var Schema string

var ErrNilDB = errors.New("nil db")
var ErrSnapshotNotFound = errors.New("snapshot not found")

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, ErrNilDB
	}
	return &Store{pool: pool}, nil
}

func (s *Store) ApplySchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, Schema)
	return err
}

func (s *Store) SaveEvents(ctx context.Context, events []matching.Event) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for i, event := range events {
		if err := saveEvent(ctx, tx, event, i); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (s *Store) SaveSnapshot(ctx context.Context, snapshot matching.Snapshot) error {
	orders, err := json.Marshal(snapshot.Orders)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, insertSnapshotSQL, snapshot.Symbol, snapshot.Sequence, orders)
	return err
}

func (s *Store) LatestSnapshot(ctx context.Context, symbol domain.Symbol) (matching.Snapshot, error) {
	var snapshot matching.Snapshot
	var ordersJSON []byte
	row := s.pool.QueryRow(ctx, selectLatestSnapshotSQL, symbol)
	if err := row.Scan(&snapshot.Symbol, &snapshot.Sequence, &ordersJSON); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return matching.Snapshot{}, ErrSnapshotNotFound
		}
		return matching.Snapshot{}, err
	}
	if err := json.Unmarshal(ordersJSON, &snapshot.Orders); err != nil {
		return matching.Snapshot{}, err
	}
	return snapshot, nil
}

func (s *Store) EventsAfter(ctx context.Context, sequence domain.Sequence) ([]matching.Event, error) {
	rows, err := s.pool.Query(ctx, selectEventsAfterSQL, sequence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEvents(rows)
}

func (s *Store) EventsBySymbol(ctx context.Context, symbol domain.Symbol, limit int) ([]matching.Event, error) {
	rows, err := s.pool.Query(ctx, selectEventsBySymbolSQL, symbol, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEvents(rows)
}

func (s *Store) TradesBySymbol(ctx context.Context, symbol domain.Symbol) ([]matching.Trade, error) {
	rows, err := s.pool.Query(ctx, selectTradesBySymbolSQL, symbol)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	trades := make([]matching.Trade, 0)
	for rows.Next() {
		var trade matching.Trade
		if err := rows.Scan(
			&trade.Sequence,
			&trade.Symbol,
			&trade.MakerOrderID,
			&trade.TakerOrderID,
			&trade.Price,
			&trade.Quantity,
		); err != nil {
			return nil, err
		}
		trades = append(trades, trade)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return trades, nil
}

func saveEvent(ctx context.Context, tx pgx.Tx, event matching.Event, index int) error {
	row := eventRow(event, index)
	if _, err := tx.Exec(ctx, insertEventSQL,
		row.Sequence,
		row.Index,
		row.Type,
		row.Symbol,
		row.OrderID,
		row.Side,
		row.OrderType,
		row.Price,
		row.Quantity,
		row.MakerOrderID,
		row.TakerOrderID,
	); err != nil {
		return err
	}

	switch event.Type {
	case domain.EventTypeOrderAccepted:
		return saveAcceptedOrder(ctx, tx, event)
	case domain.EventTypeOrderRested:
		return saveRestedOrder(ctx, tx, event)
	case domain.EventTypeTradeExecuted:
		return saveTrade(ctx, tx, event, index)
	case domain.EventTypeOrderCanceled:
		return saveCanceledOrder(ctx, tx, event)
	default:
		return nil
	}
}

func scanEvents(rows pgx.Rows) ([]matching.Event, error) {
	events := make([]matching.Event, 0)
	for rows.Next() {
		var row eventQueryRow
		if err := rows.Scan(
			&row.Sequence,
			&row.Index,
			&row.Type,
			&row.Symbol,
			&row.OrderID,
			&row.Side,
			&row.OrderType,
			&row.Price,
			&row.Quantity,
			&row.MakerOrderID,
			&row.TakerOrderID,
		); err != nil {
			return nil, err
		}
		events = append(events, eventFromRow(row))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func saveAcceptedOrder(ctx context.Context, tx pgx.Tx, event matching.Event) error {
	if event.Order == nil {
		return nil
	}
	_, err := tx.Exec(ctx, upsertAcceptedOrderSQL,
		event.Order.ID,
		event.Order.Symbol,
		event.Order.Side,
		event.Order.Type,
		nullableMoney(event.Order.Price),
		event.Order.Quantity,
		event.Order.Quantity,
		event.Sequence,
		event.Sequence,
	)
	return err
}

func saveRestedOrder(ctx context.Context, tx pgx.Tx, event matching.Event) error {
	if event.Order == nil {
		return nil
	}
	_, err := tx.Exec(ctx, updateOrderRestedSQL,
		event.Order.Quantity,
		event.Sequence,
		event.Order.ID,
	)
	return err
}

func saveTrade(ctx context.Context, tx pgx.Tx, event matching.Event, index int) error {
	if event.Trade == nil {
		return nil
	}
	if _, err := tx.Exec(ctx, insertTradeSQL,
		event.Trade.Sequence,
		index,
		event.Trade.Symbol,
		event.Trade.MakerOrderID,
		event.Trade.TakerOrderID,
		event.Trade.Price,
		event.Trade.Quantity,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, reduceMakerOrderSQL,
		event.Trade.Quantity,
		event.Sequence,
		event.Trade.MakerOrderID,
	); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, reduceTakerOrderSQL,
		event.Trade.Quantity,
		event.Sequence,
		event.Trade.TakerOrderID,
	)
	return err
}

func saveCanceledOrder(ctx context.Context, tx pgx.Tx, event matching.Event) error {
	if event.Cancel == nil {
		return nil
	}
	_, err := tx.Exec(ctx, cancelOrderSQL,
		event.Sequence,
		event.Cancel.ID,
	)
	return err
}

type persistedEvent struct {
	Sequence     domain.Sequence
	Index        int
	Type         domain.EventType
	Symbol       any
	OrderID      any
	Side         any
	OrderType    any
	Price        any
	Quantity     any
	MakerOrderID any
	TakerOrderID any
}

type eventQueryRow struct {
	Sequence     domain.Sequence
	Index        int
	Type         domain.EventType
	Symbol       sql.NullString
	OrderID      sql.NullString
	Side         sql.NullString
	OrderType    sql.NullString
	Price        sql.NullInt64
	Quantity     sql.NullInt64
	MakerOrderID sql.NullString
	TakerOrderID sql.NullString
}

func eventFromRow(row eventQueryRow) matching.Event {
	event := matching.Event{
		Type:     row.Type,
		Sequence: row.Sequence,
	}
	switch row.Type {
	case domain.EventTypeOrderAccepted, domain.EventTypeOrderRested:
		event.Order = &matching.Order{
			ID:       domain.OrderID(row.OrderID.String),
			Symbol:   domain.Symbol(row.Symbol.String),
			Side:     domain.Side(row.Side.String),
			Type:     domain.OrderType(row.OrderType.String),
			Price:    domain.Money(row.Price.Int64),
			Quantity: domain.Quantity(row.Quantity.Int64),
		}
	case domain.EventTypeTradeExecuted:
		event.Trade = &matching.Trade{
			Sequence:     row.Sequence,
			Symbol:       domain.Symbol(row.Symbol.String),
			MakerOrderID: domain.OrderID(row.MakerOrderID.String),
			TakerOrderID: domain.OrderID(row.TakerOrderID.String),
			Price:        domain.Money(row.Price.Int64),
			Quantity:     domain.Quantity(row.Quantity.Int64),
		}
	case domain.EventTypeOrderCanceled:
		event.Cancel = &matching.CanceledOrder{
			ID:       domain.OrderID(row.OrderID.String),
			Symbol:   domain.Symbol(row.Symbol.String),
			Side:     domain.Side(row.Side.String),
			Price:    domain.Money(row.Price.Int64),
			Quantity: domain.Quantity(row.Quantity.Int64),
		}
	}
	return event
}

func eventRow(event matching.Event, index int) persistedEvent {
	row := persistedEvent{
		Sequence: event.Sequence,
		Index:    index,
		Type:     event.Type,
	}

	if event.Order != nil {
		row.Symbol = nullableString(string(event.Order.Symbol))
		row.OrderID = nullableString(string(event.Order.ID))
		row.Side = nullableString(string(event.Order.Side))
		row.OrderType = nullableString(string(event.Order.Type))
		row.Price = nullableMoney(event.Order.Price)
		row.Quantity = nullableQuantity(event.Order.Quantity)
	}
	if event.Trade != nil {
		row.Symbol = nullableString(string(event.Trade.Symbol))
		row.MakerOrderID = nullableString(string(event.Trade.MakerOrderID))
		row.TakerOrderID = nullableString(string(event.Trade.TakerOrderID))
		row.Price = nullableMoney(event.Trade.Price)
		row.Quantity = nullableQuantity(event.Trade.Quantity)
	}
	if event.Cancel != nil {
		row.Symbol = nullableString(string(event.Cancel.Symbol))
		row.OrderID = nullableString(string(event.Cancel.ID))
		row.Side = nullableString(string(event.Cancel.Side))
		row.Price = nullableMoney(event.Cancel.Price)
		row.Quantity = nullableQuantity(event.Cancel.Quantity)
	}

	return row
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableMoney(value domain.Money) any {
	if value == 0 {
		return nil
	}
	return int64(value)
}

func nullableQuantity(value domain.Quantity) any {
	if value == 0 {
		return nil
	}
	return int64(value)
}
