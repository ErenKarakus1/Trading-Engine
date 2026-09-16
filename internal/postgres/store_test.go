package postgres

import (
	"strings"
	"testing"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
	"github.com/ErenKarakus1/Trading-Engine/internal/matching"
)

func TestNewStoreRejectsNilDB(t *testing.T) {
	if _, err := NewStore(nil); err != ErrNilDB {
		t.Fatalf("NewStore(nil) error = %v, want %v", err, ErrNilDB)
	}
}

func TestEventRowFromTrade(t *testing.T) {
	row := eventRow(matching.Event{
		Type:     domain.EventTypeTradeExecuted,
		Sequence: 10,
		Trade: &matching.Trade{
			Sequence:     10,
			Symbol:       "BTC-USD",
			MakerOrderID: "sell-1",
			TakerOrderID: "buy-1",
			Price:        100,
			Quantity:     4,
		},
	}, 2)

	if row.Sequence != 10 || row.Index != 2 || row.Type != domain.EventTypeTradeExecuted {
		t.Fatalf("eventRow() identity = %+v", row)
	}
	assertNullString(t, row.Symbol, "BTC-USD")
	assertNullString(t, row.MakerOrderID, "sell-1")
	assertNullString(t, row.TakerOrderID, "buy-1")
	assertNullInt64(t, row.Price, 100)
	assertNullInt64(t, row.Quantity, 4)
}

func TestSchemaContainsCoreTables(t *testing.T) {
	for _, table := range []string{"engine_events", "orders", "trades", "accounts", "positions"} {
		if !strings.Contains(Schema, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("Schema does not create %s", table)
		}
	}
}

func assertNullString(t *testing.T, got any, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("NullString = %+v, want %q", got, want)
	}
}

func assertNullInt64(t *testing.T, got any, want int64) {
	t.Helper()
	if got != want {
		t.Fatalf("NullInt64 = %+v, want %d", got, want)
	}
}
