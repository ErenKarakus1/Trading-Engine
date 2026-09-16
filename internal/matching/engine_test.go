package matching

import (
	"testing"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
)

func TestLimitOrderRestsWhenItDoesNotCross(t *testing.T) {
	engine := NewEngine()

	result, err := engine.Submit(limit("buy-1", domain.SideBuy, 100, 10))
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	if len(result.Trades) != 0 {
		t.Fatalf("len(Trades) = %d, want 0", len(result.Trades))
	}
	if !result.Rested {
		t.Fatal("Rested = false, want true")
	}

	bestBid, ok := engine.Book().BestBid()
	if !ok {
		t.Fatal("BestBid() ok = false, want true")
	}
	if bestBid.Price != 100 || bestBid.Quantity != 10 || bestBid.Orders != 1 {
		t.Fatalf("BestBid() = %+v, want price 100 quantity 10 orders 1", bestBid)
	}
}

func TestLimitOrderMatchesAtRestingOrderPrice(t *testing.T) {
	engine := NewEngine()
	mustSubmit(t, engine, limit("sell-1", domain.SideSell, 100, 10))

	result, err := engine.Submit(limit("buy-1", domain.SideBuy, 105, 4))
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	if result.Rested {
		t.Fatal("Rested = true, want false")
	}
	assertTrades(t, result.Trades, Trade{
		MakerOrderID: "sell-1",
		TakerOrderID: "buy-1",
		Price:        100,
		Quantity:     4,
	})

	bestAsk, ok := engine.Book().BestAsk()
	if !ok {
		t.Fatal("BestAsk() ok = false, want true")
	}
	if bestAsk.Quantity != 6 {
		t.Fatalf("BestAsk().Quantity = %d, want 6", bestAsk.Quantity)
	}
}

func TestPriceTimePriority(t *testing.T) {
	engine := NewEngine()
	mustSubmit(t, engine, limit("sell-1", domain.SideSell, 100, 5))
	mustSubmit(t, engine, limit("sell-2", domain.SideSell, 99, 5))
	mustSubmit(t, engine, limit("sell-3", domain.SideSell, 99, 5))

	result, err := engine.Submit(limit("buy-1", domain.SideBuy, 100, 12))
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	assertTrades(t, result.Trades,
		Trade{MakerOrderID: "sell-2", TakerOrderID: "buy-1", Price: 99, Quantity: 5},
		Trade{MakerOrderID: "sell-3", TakerOrderID: "buy-1", Price: 99, Quantity: 5},
		Trade{MakerOrderID: "sell-1", TakerOrderID: "buy-1", Price: 100, Quantity: 2},
	)
}

func TestMarketOrderDoesNotRest(t *testing.T) {
	engine := NewEngine()
	mustSubmit(t, engine, limit("sell-1", domain.SideSell, 100, 5))

	result, err := engine.Submit(market("buy-1", domain.SideBuy, 10))
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	if result.Rested {
		t.Fatal("Rested = true, want false")
	}
	if result.Remaining != 5 {
		t.Fatalf("Remaining = %d, want 5", result.Remaining)
	}
	if _, ok := engine.Book().BestBid(); ok {
		t.Fatal("BestBid() ok = true, want false")
	}
}

func TestCancelRestingOrder(t *testing.T) {
	engine := NewEngine()
	mustSubmit(t, engine, limit("buy-1", domain.SideBuy, 100, 10))

	order, err := engine.Book().Cancel("buy-1")
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if order.ID != "buy-1" {
		t.Fatalf("Cancel() ID = %q, want buy-1", order.ID)
	}
	if _, ok := engine.Book().BestBid(); ok {
		t.Fatal("BestBid() ok = true, want false")
	}
}

func limit(id domain.OrderID, side domain.Side, price domain.Money, quantity domain.Quantity) Order {
	return Order{
		ID:       id,
		Side:     side,
		Type:     domain.OrderTypeLimit,
		Price:    price,
		Quantity: quantity,
	}
}

func market(id domain.OrderID, side domain.Side, quantity domain.Quantity) Order {
	return Order{
		ID:       id,
		Side:     side,
		Type:     domain.OrderTypeMarket,
		Quantity: quantity,
	}
}

func mustSubmit(t *testing.T, engine *Engine, order Order) {
	t.Helper()
	if _, err := engine.Submit(order); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
}

func assertTrades(t *testing.T, got []Trade, want ...Trade) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len(Trades) = %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Trades[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
