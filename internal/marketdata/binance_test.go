package marketdata

import (
	"testing"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
)

func TestBinanceDepthFeedParsesSnapshot(t *testing.T) {
	feed := &BinanceDepthFeed{config: normalizeBinanceConfig(BinanceDepthConfig{
		StreamSymbol:  "btcusdt",
		DomainSymbol:  "BTC-USD",
		PriceScale:    100_000_000,
		QuantityScale: 1000,
	})}

	message, err := feed.parse([]byte(`{"lastUpdateId":160,"bids":[["0.0024","10"]],"asks":[["0.0026","100"]]}`))
	if err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	if message.Type != MessageTypeSnapshot || message.Snapshot == nil {
		t.Fatalf("message = %+v, want snapshot", message)
	}
	if message.Snapshot.Sequence != 160 || message.Snapshot.Symbol != "BTC-USD" {
		t.Fatalf("snapshot = %+v", message.Snapshot)
	}
	assertLevels(t, message.Snapshot.Bids, []Level{{Price: 240000, Quantity: 10000}})
}

func TestBinanceDepthFeedParsesDiffUpdate(t *testing.T) {
	feed := &BinanceDepthFeed{config: normalizeBinanceConfig(BinanceDepthConfig{
		DomainSymbol:  "BTC-USD",
		PriceScale:    100_000_000,
		QuantityScale: 1000,
	})}

	message, err := feed.parse([]byte(`{"e":"depthUpdate","E":1672515782136,"s":"BNBBTC","U":157,"u":160,"b":[["0.0024","10"]],"a":[["0.0026","100"]]}`))
	if err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	if message.Type != MessageTypeUpdate || message.Update == nil {
		t.Fatalf("message = %+v, want update", message)
	}
	if message.Update.FirstSequence != 157 || message.Update.Sequence != 160 {
		t.Fatalf("update = %+v", message.Update)
	}
	assertLevels(t, message.Update.Bids, []Level{{Price: domain.Money(240000), Quantity: 10000}})
}

func TestBinanceDepthFeedParsesLevels(t *testing.T) {
	feed := &BinanceDepthFeed{config: normalizeBinanceConfig(BinanceDepthConfig{
		PriceScale:    100_000_000,
		QuantityScale: 1000,
	})}

	levels, err := feed.parseLevels([][]string{{"0.0026", "100"}})
	if err != nil {
		t.Fatalf("parseLevels() error = %v", err)
	}
	assertLevels(t, levels, []Level{{Price: 260000, Quantity: 100000}})
}

func TestParseScaledDecimalUsesIntegerMath(t *testing.T) {
	value, err := parseScaledDecimal("123.45678901", 100_000_000)
	if err != nil {
		t.Fatalf("parseScaledDecimal() error = %v", err)
	}
	if value != 12_345_678_901 {
		t.Fatalf("value = %d, want 12345678901", value)
	}

	value, err = parseScaledDecimal("0.0024", 100_000_000)
	if err != nil {
		t.Fatalf("parseScaledDecimal() small decimal error = %v", err)
	}
	if value != 240_000 {
		t.Fatalf("small decimal value = %d, want 240000", value)
	}
}

func TestBinanceUpdateSequenceRangeApplies(t *testing.T) {
	book := NewBook("BTC-USD")
	if err := book.ApplySnapshot(Snapshot{Symbol: "BTC-USD", Sequence: 156, Bids: []Level{{Price: 100, Quantity: 1}}}); err != nil {
		t.Fatalf("ApplySnapshot() error = %v", err)
	}
	if err := book.ApplyUpdate(Update{Symbol: "BTC-USD", FirstSequence: 157, Sequence: 160, Bids: []Level{{Price: 101, Quantity: 2}}}); err != nil {
		t.Fatalf("ApplyUpdate() error = %v", err)
	}
}
