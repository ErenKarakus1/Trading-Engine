package risk

import (
	"errors"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
	"github.com/ErenKarakus1/Trading-Engine/internal/matching"
)

var (
	ErrInsufficientBalance  = errors.New("insufficient balance")
	ErrInsufficientPosition = errors.New("insufficient position")
	ErrMaxOrderQuantity     = errors.New("max order quantity exceeded")
	ErrMaxPosition          = errors.New("max position exceeded")
	ErrPriceCollar          = errors.New("price outside collar")
)

type AccountID string

type Account struct {
	ID        AccountID
	Cash      domain.Money
	Positions map[domain.Symbol]domain.Quantity
}

type Config struct {
	MaxOrderQuantity domain.Quantity
	MaxPosition      domain.Quantity
	PriceCollars     map[domain.Symbol]PriceCollar
}

type PriceCollar struct {
	Min domain.Money
	Max domain.Money
}

type Engine struct {
	config Config
}

func NewEngine(config Config) *Engine {
	return &Engine{config: config}
}

func (e *Engine) Check(account Account, order matching.Order) error {
	if err := e.checkMaxOrderQuantity(order); err != nil {
		return err
	}
	if err := e.checkPriceCollar(order); err != nil {
		return err
	}
	if err := e.checkBalance(account, order); err != nil {
		return err
	}
	return e.checkMaxPosition(account, order)
}

func (e *Engine) checkMaxOrderQuantity(order matching.Order) error {
	if e.config.MaxOrderQuantity <= 0 {
		return nil
	}
	if order.Quantity > e.config.MaxOrderQuantity {
		return ErrMaxOrderQuantity
	}
	return nil
}

func (e *Engine) checkPriceCollar(order matching.Order) error {
	if order.Type == domain.OrderTypeMarket {
		return nil
	}

	collar, exists := e.config.PriceCollars[order.Symbol]
	if !exists {
		return nil
	}
	if collar.Min > 0 && order.Price < collar.Min {
		return ErrPriceCollar
	}
	if collar.Max > 0 && order.Price > collar.Max {
		return ErrPriceCollar
	}
	return nil
}

func (e *Engine) checkBalance(account Account, order matching.Order) error {
	if order.Side != domain.SideBuy || order.Type == domain.OrderTypeMarket {
		return nil
	}
	if notional(order) > account.Cash {
		return ErrInsufficientBalance
	}
	return nil
}

func (e *Engine) checkMaxPosition(account Account, order matching.Order) error {
	current := account.Positions[order.Symbol]
	if order.Side == domain.SideSell {
		if order.Quantity > current {
			return ErrInsufficientPosition
		}
		return nil
	}

	if e.config.MaxPosition <= 0 {
		return nil
	}
	if current+order.Quantity > e.config.MaxPosition {
		return ErrMaxPosition
	}
	return nil
}

func notional(order matching.Order) domain.Money {
	return domain.Money(int64(order.Price) * int64(order.Quantity))
}
