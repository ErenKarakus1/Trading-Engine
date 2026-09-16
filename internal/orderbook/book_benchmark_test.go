package orderbook

import (
	"fmt"
	"testing"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
)

func BenchmarkAddOrdersAcrossPriceLevels(b *testing.B) {
	book := New()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := book.Add(Order{
			ID:       domain.OrderID(fmt.Sprintf("buy-%d", i)),
			Side:     domain.SideBuy,
			Price:    domain.Money(100 + i%100),
			Quantity: 1,
		})
		if err != nil {
			b.Fatalf("Add() error = %v", err)
		}
	}
}

func BenchmarkBestBid(b *testing.B) {
	book := New()
	for i := 0; i < 10_000; i++ {
		err := book.Add(Order{
			ID:       domain.OrderID(fmt.Sprintf("buy-%d", i)),
			Side:     domain.SideBuy,
			Price:    domain.Money(100 + i%100),
			Quantity: 1,
		})
		if err != nil {
			b.Fatalf("Add() error = %v", err)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if _, ok := book.BestBid(); !ok {
			b.Fatal("BestBid() ok = false")
		}
	}
}

func BenchmarkCancelFIFOLevel(b *testing.B) {
	book := New()
	for i := 0; i < b.N; i++ {
		err := book.Add(Order{
			ID:       domain.OrderID(fmt.Sprintf("buy-%d", i)),
			Side:     domain.SideBuy,
			Price:    100,
			Quantity: 1,
		})
		if err != nil {
			b.Fatalf("Add() error = %v", err)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if _, err := book.Cancel(domain.OrderID(fmt.Sprintf("buy-%d", i))); err != nil {
			b.Fatalf("Cancel() error = %v", err)
		}
	}
}
