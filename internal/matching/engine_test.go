package matching

import (
	"fmt"
	"sync"
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
	if result.Sequence != 1 {
		t.Fatalf("Sequence = %d, want 1", result.Sequence)
	}
	if !result.Rested {
		t.Fatal("Rested = false, want true")
	}

	bestBid, ok := engine.BestBid("BTC-USD")
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
	if result.Sequence != 2 {
		t.Fatalf("Sequence = %d, want 2", result.Sequence)
	}
	assertTrades(t, result.Trades, Trade{
		Sequence:     2,
		Symbol:       "BTC-USD",
		MakerOrderID: "sell-1",
		TakerOrderID: "buy-1",
		Price:        100,
		Quantity:     4,
	})

	bestAsk, ok := engine.BestAsk("BTC-USD")
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
		Trade{Sequence: 4, Symbol: "BTC-USD", MakerOrderID: "sell-2", TakerOrderID: "buy-1", Price: 99, Quantity: 5},
		Trade{Sequence: 4, Symbol: "BTC-USD", MakerOrderID: "sell-3", TakerOrderID: "buy-1", Price: 99, Quantity: 5},
		Trade{Sequence: 4, Symbol: "BTC-USD", MakerOrderID: "sell-1", TakerOrderID: "buy-1", Price: 100, Quantity: 2},
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
	if result.Sequence != 2 {
		t.Fatalf("Sequence = %d, want 2", result.Sequence)
	}
	if result.Remaining != 5 {
		t.Fatalf("Remaining = %d, want 5", result.Remaining)
	}
	if _, ok := engine.BestBid("BTC-USD"); ok {
		t.Fatal("BestBid() ok = true, want false")
	}
}

func TestSymbolsAreIsolated(t *testing.T) {
	engine := NewEngine()
	mustSubmit(t, engine, limitForSymbol("btc-sell-1", "BTC-USD", domain.SideSell, 100, 5))
	mustSubmit(t, engine, limitForSymbol("eth-buy-1", "ETH-USD", domain.SideBuy, 150, 5))

	result, err := engine.Submit(limitForSymbol("eth-sell-1", "ETH-USD", domain.SideSell, 150, 3))
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	assertTrades(t, result.Trades,
		Trade{Sequence: 3, Symbol: "ETH-USD", MakerOrderID: "eth-buy-1", TakerOrderID: "eth-sell-1", Price: 150, Quantity: 3},
	)

	btcAsk, ok := engine.BestAsk("BTC-USD")
	if !ok {
		t.Fatal("BTC BestAsk() ok = false, want true")
	}
	if btcAsk.Quantity != 5 {
		t.Fatalf("BTC BestAsk().Quantity = %d, want 5", btcAsk.Quantity)
	}
}

func TestCancelRestingOrder(t *testing.T) {
	engine := NewEngine()
	mustSubmit(t, engine, limit("buy-1", domain.SideBuy, 100, 10))

	result, err := engine.Cancel("BTC-USD", "buy-1")
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if result.Sequence != 2 {
		t.Fatalf("Sequence = %d, want 2", result.Sequence)
	}
	if result.Order.ID != "buy-1" {
		t.Fatalf("Cancel() ID = %q, want buy-1", result.Order.ID)
	}
	if _, ok := engine.BestBid("BTC-USD"); ok {
		t.Fatal("BestBid() ok = true, want false")
	}
}

func TestRejectedOrdersDoNotConsumeSequenceNumbers(t *testing.T) {
	engine := NewEngine()

	if _, err := engine.Submit(limit("", domain.SideBuy, 100, 1)); err == nil {
		t.Fatal("Submit() error = nil, want error")
	}

	result, err := engine.Submit(limit("buy-1", domain.SideBuy, 100, 1))
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if result.Sequence != 1 {
		t.Fatalf("Sequence = %d, want 1", result.Sequence)
	}
}

func TestConcurrentSubmissionsAreSafe(t *testing.T) {
	engine := NewEngine()
	const orders = 100

	var wg sync.WaitGroup
	for i := range orders {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			symbol := domain.Symbol("BTC-USD")
			if i%2 == 0 {
				symbol = "ETH-USD"
			}

			order := limitForSymbol(
				domain.OrderID(fmt.Sprintf("buy-%d", i)),
				symbol,
				domain.SideBuy,
				100,
				1,
			)
			if _, err := engine.Submit(order); err != nil {
				t.Errorf("Submit() error = %v", err)
			}
		}(i)
	}
	wg.Wait()

	btcBid, ok := engine.BestBid("BTC-USD")
	if !ok {
		t.Fatal("BTC BestBid() ok = false, want true")
	}
	if btcBid.Quantity != 50 {
		t.Fatalf("BTC BestBid().Quantity = %d, want 50", btcBid.Quantity)
	}

	ethBid, ok := engine.BestBid("ETH-USD")
	if !ok {
		t.Fatal("ETH BestBid() ok = false, want true")
	}
	if ethBid.Quantity != 50 {
		t.Fatalf("ETH BestBid().Quantity = %d, want 50", ethBid.Quantity)
	}
}

func limit(id domain.OrderID, side domain.Side, price domain.Money, quantity domain.Quantity) Order {
	return limitForSymbol(id, "BTC-USD", side, price, quantity)
}

func limitForSymbol(id domain.OrderID, symbol domain.Symbol, side domain.Side, price domain.Money, quantity domain.Quantity) Order {
	return Order{
		ID:       id,
		Symbol:   symbol,
		Side:     side,
		Type:     domain.OrderTypeLimit,
		Price:    price,
		Quantity: quantity,
	}
}

func market(id domain.OrderID, side domain.Side, quantity domain.Quantity) Order {
	return Order{
		ID:       id,
		Symbol:   "BTC-USD",
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
