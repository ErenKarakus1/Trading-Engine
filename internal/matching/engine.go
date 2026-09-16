package matching

import (
	"errors"
	"sync"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
	"github.com/ErenKarakus1/Trading-Engine/internal/orderbook"
)

var ErrInvalidOrder = errors.New("invalid order")

type Order struct {
	ID       domain.OrderID
	Symbol   domain.Symbol
	Side     domain.Side
	Type     domain.OrderType
	Price    domain.Money
	Quantity domain.Quantity
}

type Trade struct {
	Symbol       domain.Symbol
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
	mu    sync.Mutex
	books map[domain.Symbol]*symbolBook
}

type symbolBook struct {
	mu   sync.Mutex
	book *orderbook.Book
}

func NewEngine() *Engine {
	return &Engine{books: make(map[domain.Symbol]*symbolBook)}
}

func (e *Engine) Submit(order Order) (Result, error) {
	if err := validate(order); err != nil {
		return Result{}, err
	}
	symbolBook := e.symbolBook(order.Symbol)
	symbolBook.mu.Lock()
	defer symbolBook.mu.Unlock()

	result := Result{
		Accepted:  order,
		Remaining: order.Quantity,
	}
	book := symbolBook.book

	for result.Remaining > 0 {
		maker, ok := book.BestOrder(opposite(order.Side))
		if !ok || !crosses(order, maker.Price) {
			break
		}

		tradeQuantity := minQuantity(result.Remaining, maker.Quantity)
		if _, err := book.Reduce(maker.ID, tradeQuantity); err != nil {
			return Result{}, err
		}

		result.Trades = append(result.Trades, Trade{
			Symbol:       order.Symbol,
			MakerOrderID: maker.ID,
			TakerOrderID: order.ID,
			Price:        maker.Price,
			Quantity:     tradeQuantity,
		})
		result.Remaining -= tradeQuantity
	}

	if order.Type == domain.OrderTypeLimit && result.Remaining > 0 {
		if err := book.Add(orderbook.Order{
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

func (e *Engine) Cancel(symbol domain.Symbol, orderID domain.OrderID) (orderbook.Order, error) {
	symbolBook := e.symbolBook(symbol)
	symbolBook.mu.Lock()
	defer symbolBook.mu.Unlock()

	return symbolBook.book.Cancel(orderID)
}

func (e *Engine) BestBid(symbol domain.Symbol) (orderbook.PriceLevel, bool) {
	symbolBook := e.symbolBook(symbol)
	symbolBook.mu.Lock()
	defer symbolBook.mu.Unlock()

	return symbolBook.book.BestBid()
}

func (e *Engine) BestAsk(symbol domain.Symbol) (orderbook.PriceLevel, bool) {
	symbolBook := e.symbolBook(symbol)
	symbolBook.mu.Lock()
	defer symbolBook.mu.Unlock()

	return symbolBook.book.BestAsk()
}

func (e *Engine) symbolBook(symbol domain.Symbol) *symbolBook {
	e.mu.Lock()
	defer e.mu.Unlock()

	book, exists := e.books[symbol]
	if !exists {
		book = &symbolBook{book: orderbook.New()}
		e.books[symbol] = book
	}
	return book
}

func validate(order Order) error {
	if order.ID == "" || order.Symbol == "" || order.Quantity <= 0 {
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
