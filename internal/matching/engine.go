package matching

import (
	"errors"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
	"github.com/ErenKarakus1/Trading-Engine/internal/orderbook"
)

var ErrInvalidOrder = errors.New("invalid order")

type Order struct {
	ID       domain.OrderID
	Side     domain.Side
	Type     domain.OrderType
	Price    domain.Money
	Quantity domain.Quantity
}

type Trade struct {
	MakerOrderID domain.OrderID
	TakerOrderID domain.OrderID
	Price        domain.Money
	Quantity     domain.Quantity
}

type Result struct {
	Accepted  Order
	Trades    []Trade
	Remaining domain.Quantity
	Rested    bool
}

type Engine struct {
	book *orderbook.Book
}

func NewEngine() *Engine {
	return &Engine{book: orderbook.New()}
}

func (e *Engine) Submit(order Order) (Result, error) {
	if err := validate(order); err != nil {
		return Result{}, err
	}

	result := Result{
		Accepted:  order,
		Remaining: order.Quantity,
	}

	for result.Remaining > 0 {
		maker, ok := e.book.BestOrder(opposite(order.Side))
		if !ok || !crosses(order, maker.Price) {
			break
		}

		tradeQuantity := minQuantity(result.Remaining, maker.Quantity)
		if _, err := e.book.Reduce(maker.ID, tradeQuantity); err != nil {
			return Result{}, err
		}

		result.Trades = append(result.Trades, Trade{
			MakerOrderID: maker.ID,
			TakerOrderID: order.ID,
			Price:        maker.Price,
			Quantity:     tradeQuantity,
		})
		result.Remaining -= tradeQuantity
	}

	if order.Type == domain.OrderTypeLimit && result.Remaining > 0 {
		if err := e.book.Add(orderbook.Order{
			ID:       order.ID,
			Side:     order.Side,
			Price:    order.Price,
			Quantity: result.Remaining,
		}); err != nil {
			return Result{}, err
		}
		result.Rested = true
	}

	return result, nil
}

func (e *Engine) Book() *orderbook.Book {
	return e.book
}

func validate(order Order) error {
	if order.ID == "" || order.Quantity <= 0 {
		return ErrInvalidOrder
	}
	if order.Side != domain.SideBuy && order.Side != domain.SideSell {
		return ErrInvalidOrder
	}
	if order.Type != domain.OrderTypeLimit && order.Type != domain.OrderTypeMarket {
		return ErrInvalidOrder
	}
	if order.Type == domain.OrderTypeLimit && order.Price <= 0 {
		return ErrInvalidOrder
	}
	if order.Type == domain.OrderTypeMarket && order.Price != 0 {
		return ErrInvalidOrder
	}
	return nil
}

func crosses(taker Order, makerPrice domain.Money) bool {
	if taker.Type == domain.OrderTypeMarket {
		return true
	}
	if taker.Side == domain.SideBuy {
		return taker.Price >= makerPrice
	}
	return taker.Price <= makerPrice
}

func opposite(side domain.Side) domain.Side {
	if side == domain.SideBuy {
		return domain.SideSell
	}
	return domain.SideBuy
}

func minQuantity(a, b domain.Quantity) domain.Quantity {
	if a < b {
		return a
	}
	return b
}
