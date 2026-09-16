package matching

import (
	"fmt"
	"testing"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
)

func BenchmarkSubmitRestingLimitOrder(b *testing.B) {
	engine := NewEngine()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		order := Order{
			ID:       domain.OrderID(fmt.Sprintf("buy-%d", i)),
			Symbol:   "BTC-USD",
			Side:     domain.SideBuy,
			Type:     domain.OrderTypeLimit,
			Price:    domain.Money(100 + i%100),
			Quantity: 1,
		}
		if _, err := engine.Submit(order); err != nil {
			b.Fatalf("Submit() error = %v", err)
		}
	}
}

func BenchmarkSubmitCrossingLimitOrder(b *testing.B) {
	engine := NewEngine()
	for i := 0; i < b.N; i++ {
		seedOrder := Order{
			ID:       domain.OrderID(fmt.Sprintf("sell-%d", i)),
			Symbol:   "BTC-USD",
			Side:     domain.SideSell,
			Type:     domain.OrderTypeLimit,
			Price:    100,
			Quantity: 1,
		}
		if _, err := engine.Submit(seedOrder); err != nil {
			b.Fatalf("seed Submit() error = %v", err)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		order := Order{
			ID:       domain.OrderID(fmt.Sprintf("buy-%d", i)),
			Symbol:   "BTC-USD",
			Side:     domain.SideBuy,
			Type:     domain.OrderTypeLimit,
			Price:    100,
			Quantity: 1,
		}
		if _, err := engine.Submit(order); err != nil {
			b.Fatalf("Submit() error = %v", err)
		}
	}
}

func BenchmarkSubmitMarketOrderManyFills(b *testing.B) {
	const fillsPerOrder = 10
	engine := NewEngine()
	for i := 0; i < b.N*fillsPerOrder; i++ {
		seedOrder := Order{
			ID:       domain.OrderID(fmt.Sprintf("sell-%d", i)),
			Symbol:   "BTC-USD",
			Side:     domain.SideSell,
			Type:     domain.OrderTypeLimit,
			Price:    domain.Money(100 + i%fillsPerOrder),
			Quantity: 1,
		}
		if _, err := engine.Submit(seedOrder); err != nil {
			b.Fatalf("seed Submit() error = %v", err)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		order := Order{
			ID:       domain.OrderID(fmt.Sprintf("market-buy-%d", i)),
			Symbol:   "BTC-USD",
			Side:     domain.SideBuy,
			Type:     domain.OrderTypeMarket,
			Quantity: fillsPerOrder,
		}
		if _, err := engine.Submit(order); err != nil {
			b.Fatalf("Submit() error = %v", err)
		}
	}
}
