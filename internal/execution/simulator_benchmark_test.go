package execution

import (
	"testing"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
	"github.com/ErenKarakus1/Trading-Engine/internal/marketdata"
)

func BenchmarkSimulateMarketOrder(b *testing.B) {
	snapshot := marketdata.Snapshot{
		Symbol: "BTC-USD",
		Bids:   make([]marketdata.Level, 100),
		Asks:   make([]marketdata.Level, 100),
	}
	for i := range 100 {
		snapshot.Bids[i] = marketdata.Level{Price: domain.Money(100 - i), Quantity: 10}
		snapshot.Asks[i] = marketdata.Level{Price: domain.Money(101 + i), Quantity: 10}
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := SimulateMarketOrder(snapshot, domain.SideBuy, 250); err != nil {
			b.Fatalf("SimulateMarketOrder() error = %v", err)
		}
	}
}
