package orderbook

import (
	"errors"
	"sort"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
)

var (
	ErrInvalidOrder     = errors.New("invalid order")
	ErrDuplicateOrderID = errors.New("duplicate order id")
	ErrOrderNotFound    = errors.New("order not found")
)

type Order struct {
	ID       domain.OrderID
	Side     domain.Side
	Price    domain.Money
	Quantity domain.Quantity
}

type Book struct {
	buys     bookSide
	sells    bookSide
	orderRef map[domain.OrderID]orderRef
}

type orderRef struct {
	side  domain.Side
	price domain.Money
	index int
}

func New() *Book {
	return &Book{
		buys:     newBookSide(domain.SideBuy),
		sells:    newBookSide(domain.SideSell),
		orderRef: make(map[domain.OrderID]orderRef),
	}
}

func (b *Book) Add(order Order) error {
	if order.ID == "" || order.Price <= 0 || order.Quantity <= 0 {
		return ErrInvalidOrder
	}
	if order.Side != domain.SideBuy && order.Side != domain.SideSell {
		return ErrInvalidOrder
	}
	if _, exists := b.orderRef[order.ID]; exists {
		return ErrDuplicateOrderID
	}

	side := b.side(order.Side)
	index := side.add(order)
	b.orderRef[order.ID] = orderRef{
		side:  order.Side,
		price: order.Price,
		index: index,
	}

	return nil
}

func (b *Book) Cancel(orderID domain.OrderID) (Order, error) {
	ref, exists := b.orderRef[orderID]
	if !exists {
		return Order{}, ErrOrderNotFound
	}

	order := b.side(ref.side).remove(ref.price, ref.index)
	delete(b.orderRef, orderID)
	b.reindexLevel(ref.side, ref.price, ref.index)

	return order, nil
}

func (b *Book) BestBid() (PriceLevel, bool) {
	return b.buys.best()
}

func (b *Book) BestAsk() (PriceLevel, bool) {
	return b.sells.best()
}

func (b *Book) side(side domain.Side) *bookSide {
	if side == domain.SideBuy {
		return &b.buys
	}
	return &b.sells
}

func (b *Book) reindexLevel(side domain.Side, price domain.Money, start int) {
	level, exists := b.side(side).levels[price]
	if !exists {
		return
	}

	for i := start; i < len(level.orders); i++ {
		ref := b.orderRef[level.orders[i].ID]
		ref.index = i
		b.orderRef[level.orders[i].ID] = ref
	}
}

type PriceLevel struct {
	Price    domain.Money
	Quantity domain.Quantity
	Orders   int
}

type priceLevel struct {
	price  domain.Money
	orders []Order
}

func (l priceLevel) snapshot() PriceLevel {
	var quantity domain.Quantity
	for _, order := range l.orders {
		quantity += order.Quantity
	}

	return PriceLevel{
		Price:    l.price,
		Quantity: quantity,
		Orders:   len(l.orders),
	}
}

type bookSide struct {
	side   domain.Side
	prices []domain.Money
	levels map[domain.Money]*priceLevel
}

func newBookSide(side domain.Side) bookSide {
	return bookSide{
		side:   side,
		levels: make(map[domain.Money]*priceLevel),
	}
}

func (s *bookSide) add(order Order) int {
	level, exists := s.levels[order.Price]
	if !exists {
		level = &priceLevel{price: order.Price}
		s.levels[order.Price] = level
		s.insertPrice(order.Price)
	}

	level.orders = append(level.orders, order)
	return len(level.orders) - 1
}

func (s *bookSide) remove(price domain.Money, index int) Order {
	level := s.levels[price]
	order := level.orders[index]
	level.orders = append(level.orders[:index], level.orders[index+1:]...)

	if len(level.orders) == 0 {
		delete(s.levels, price)
		s.removePrice(price)
	}

	return order
}

func (s *bookSide) best() (PriceLevel, bool) {
	if len(s.prices) == 0 {
		return PriceLevel{}, false
	}

	level := s.levels[s.prices[0]]
	return level.snapshot(), true
}

func (s *bookSide) insertPrice(price domain.Money) {
	s.prices = append(s.prices, price)
	sort.Slice(s.prices, func(i, j int) bool {
		if s.side == domain.SideBuy {
			return s.prices[i] > s.prices[j]
		}
		return s.prices[i] < s.prices[j]
	})
}

func (s *bookSide) removePrice(price domain.Money) {
	for i, current := range s.prices {
		if current == price {
			s.prices = append(s.prices[:i], s.prices[i+1:]...)
			return
		}
	}
}
