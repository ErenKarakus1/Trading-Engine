package risk

import (
	"errors"
	"testing"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
	"github.com/ErenKarakus1/Trading-Engine/internal/matching"
)

func TestCheckAcceptsOrderWithinLimits(t *testing.T) {
	engine := NewEngine(Config{
		MaxOrderQuantity: 10,
		MaxPosition:      20,
		PriceCollars: map[domain.Symbol]PriceCollar{
			"BTC-USD": {Min: 90, Max: 110},
		},
	})
	account := Account{
		ID:        "account-1",
		Cash:      1_000,
		Positions: map[domain.Symbol]domain.Quantity{"BTC-USD": 5},
	}

	if err := engine.Check(account, limit("buy-1", domain.SideBuy, 100, 5)); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
}

func TestCheckRejectsInsufficientBalance(t *testing.T) {
	engine := NewEngine(Config{})
	account := Account{ID: "account-1", Cash: 499}

	err := engine.Check(account, limit("buy-1", domain.SideBuy, 100, 5))
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("Check() error = %v, want %v", err, ErrInsufficientBalance)
	}
}

func TestCheckRejectsMaxOrderQuantity(t *testing.T) {
	engine := NewEngine(Config{MaxOrderQuantity: 5})
	account := Account{ID: "account-1", Cash: 1_000}

	err := engine.Check(account, limit("buy-1", domain.SideBuy, 100, 6))
	if !errors.Is(err, ErrMaxOrderQuantity) {
		t.Fatalf("Check() error = %v, want %v", err, ErrMaxOrderQuantity)
	}
}

func TestCheckRejectsMaxPosition(t *testing.T) {
	engine := NewEngine(Config{MaxPosition: 10})
	account := Account{
		ID:        "account-1",
		Cash:      1_000,
		Positions: map[domain.Symbol]domain.Quantity{"BTC-USD": 8},
	}

	err := engine.Check(account, limit("buy-1", domain.SideBuy, 100, 3))
	if !errors.Is(err, ErrMaxPosition) {
		t.Fatalf("Check() error = %v, want %v", err, ErrMaxPosition)
	}
}

func TestCheckRejectsPriceOutsideCollar(t *testing.T) {
	engine := NewEngine(Config{
		PriceCollars: map[domain.Symbol]PriceCollar{
			"BTC-USD": {Min: 90, Max: 110},
		},
	})
	account := Account{ID: "account-1", Cash: 1_000}

	err := engine.Check(account, limit("buy-1", domain.SideBuy, 111, 1))
	if !errors.Is(err, ErrPriceCollar) {
		t.Fatalf("Check() error = %v, want %v", err, ErrPriceCollar)
	}
}

func TestSellDoesNotRequireCash(t *testing.T) {
	engine := NewEngine(Config{})
	account := Account{
		ID:        "account-1",
		Positions: map[domain.Symbol]domain.Quantity{"BTC-USD": 5},
	}

	if err := engine.Check(account, limit("sell-1", domain.SideSell, 100, 5)); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
}

func TestCheckRejectsInsufficientPosition(t *testing.T) {
	engine := NewEngine(Config{})
	account := Account{
		ID:        "account-1",
		Positions: map[domain.Symbol]domain.Quantity{"BTC-USD": 4},
	}

	err := engine.Check(account, limit("sell-1", domain.SideSell, 100, 5))
	if !errors.Is(err, ErrInsufficientPosition) {
		t.Fatalf("Check() error = %v, want %v", err, ErrInsufficientPosition)
	}
}

func TestMarketOrderSkipsPriceBasedChecks(t *testing.T) {
	engine := NewEngine(Config{
		PriceCollars: map[domain.Symbol]PriceCollar{
			"BTC-USD": {Min: 90, Max: 110},
		},
	})
	account := Account{ID: "account-1"}

	if err := engine.Check(account, market("buy-1", domain.SideBuy, 5)); err != nil {
		t.Fatalf("Check() error = %v", err)
	}
}

func limit(id domain.OrderID, side domain.Side, price domain.Money, quantity domain.Quantity) matching.Order {
	return matching.Order{
		ID:       id,
		Symbol:   "BTC-USD",
		Side:     side,
		Type:     domain.OrderTypeLimit,
		Price:    price,
		Quantity: quantity,
	}
}

func market(id domain.OrderID, side domain.Side, quantity domain.Quantity) matching.Order {
	return matching.Order{
		ID:       id,
		Symbol:   "BTC-USD",
		Side:     side,
		Type:     domain.OrderTypeMarket,
		Quantity: quantity,
	}
}
