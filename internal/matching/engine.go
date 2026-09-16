package matching

import (
	"errors"
	"sync"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
	"github.com/ErenKarakus1/Trading-Engine/internal/orderbook"
)

var (
	ErrInvalidOrder = errors.New("invalid order")
	ErrInvalidEvent = errors.New("invalid event")
	ErrEventGap     = errors.New("event sequence gap")
	ErrEventOrder   = errors.New("event sequence out of order")
)

type Order struct {
	ID       domain.OrderID
	Symbol   domain.Symbol
	Side     domain.Side
	Type     domain.OrderType
	Price    domain.Money
	Quantity domain.Quantity
}

type Trade struct {
	Sequence     domain.Sequence
	Symbol       domain.Symbol
	MakerOrderID domain.OrderID
	TakerOrderID domain.OrderID
	Price        domain.Money
	Quantity     domain.Quantity
}

type Result struct {
	Sequence  domain.Sequence
	Accepted  Order
	Trades    []Trade
	Events    []Event
	Remaining domain.Quantity
	Rested    bool
}

type CancelResult struct {
	Sequence domain.Sequence
	Order    orderbook.Order
	Event    Event
}

type Event struct {
	Type     domain.EventType
	Sequence domain.Sequence
	Order    *Order
	Trade    *Trade
	Cancel   *CanceledOrder
}

type CanceledOrder struct {
	ID       domain.OrderID
	Symbol   domain.Symbol
	Side     domain.Side
	Price    domain.Money
	Quantity domain.Quantity
}

type Engine struct {
	mu           sync.Mutex
	seqMu        sync.Mutex
	nextSequence domain.Sequence
	books        map[domain.Symbol]*symbolBook
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

	result.Sequence = e.sequence()
	for i := range result.Trades {
		result.Trades[i].Sequence = result.Sequence
	}
	result.Events = submitEvents(result)

	return result, nil
}

func (e *Engine) Cancel(symbol domain.Symbol, orderID domain.OrderID) (CancelResult, error) {
	symbolBook := e.symbolBook(symbol)
	symbolBook.mu.Lock()
	defer symbolBook.mu.Unlock()

	order, err := symbolBook.book.Cancel(orderID)
	if err != nil {
		return CancelResult{}, err
	}

	result := CancelResult{
		Sequence: e.sequence(),
		Order:    order,
	}
	result.Event = Event{
		Type:     domain.EventTypeOrderCanceled,
		Sequence: result.Sequence,
		Cancel: &CanceledOrder{
			ID:       order.ID,
			Symbol:   symbol,
			Side:     order.Side,
			Price:    order.Price,
			Quantity: order.Quantity,
		},
	}

	return result, nil
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

func (e *Engine) Replay(events []Event) error {
	var last domain.Sequence
	for _, event := range events {
		if event.Sequence <= 0 {
			return ErrInvalidEvent
		}
		if last != 0 {
			if event.Sequence < last {
				return ErrEventOrder
			}
			if event.Sequence > last+1 {
				return ErrEventGap
			}
		}
		if err := e.apply(event); err != nil {
			return err
		}
		last = event.Sequence
	}

	if last > e.currentSequence() {
		e.setSequence(last)
	}
	return nil
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

func (e *Engine) sequence() domain.Sequence {
	e.seqMu.Lock()
	defer e.seqMu.Unlock()

	e.nextSequence++
	return e.nextSequence
}

func (e *Engine) currentSequence() domain.Sequence {
	e.seqMu.Lock()
	defer e.seqMu.Unlock()

	return e.nextSequence
}

func (e *Engine) setSequence(sequence domain.Sequence) {
	e.seqMu.Lock()
	defer e.seqMu.Unlock()

	e.nextSequence = sequence
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

func submitEvents(result Result) []Event {
	events := []Event{
		{
			Type:     domain.EventTypeOrderAccepted,
			Sequence: result.Sequence,
			Order:    &result.Accepted,
		},
	}

	for i := range result.Trades {
		events = append(events, Event{
			Type:     domain.EventTypeTradeExecuted,
			Sequence: result.Sequence,
			Trade:    &result.Trades[i],
		})
	}

	if result.Rested {
		rested := result.Accepted
		rested.Quantity = result.Remaining
		events = append(events, Event{
			Type:     domain.EventTypeOrderRested,
			Sequence: result.Sequence,
			Order:    &rested,
		})
	}

	return events
}

func (e *Engine) apply(event Event) error {
	switch event.Type {
	case domain.EventTypeOrderAccepted:
		if event.Order == nil {
			return ErrInvalidEvent
		}
		return nil
	case domain.EventTypeOrderRested:
		if event.Order == nil {
			return ErrInvalidEvent
		}
		symbolBook := e.symbolBook(event.Order.Symbol)
		symbolBook.mu.Lock()
		defer symbolBook.mu.Unlock()

		return symbolBook.book.Add(orderbook.Order{
			ID:       event.Order.ID,
			Side:     event.Order.Side,
			Price:    event.Order.Price,
			Quantity: event.Order.Quantity,
		})
	case domain.EventTypeTradeExecuted:
		if event.Trade == nil {
			return ErrInvalidEvent
		}
		symbolBook := e.symbolBook(event.Trade.Symbol)
		symbolBook.mu.Lock()
		defer symbolBook.mu.Unlock()

		_, err := symbolBook.book.Reduce(event.Trade.MakerOrderID, event.Trade.Quantity)
		return err
	case domain.EventTypeOrderCanceled:
		if event.Cancel == nil {
			return ErrInvalidEvent
		}
		symbolBook := e.symbolBook(event.Cancel.Symbol)
		symbolBook.mu.Lock()
		defer symbolBook.mu.Unlock()

		_, err := symbolBook.book.Cancel(event.Cancel.ID)
		return err
	default:
		return ErrInvalidEvent
	}
}
