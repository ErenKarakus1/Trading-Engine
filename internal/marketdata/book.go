package marketdata

import (
	"errors"
	"sort"
	"sync"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
)

var (
	ErrGapDetected     = errors.New("market data sequence gap")
	ErrInvalidSnapshot = errors.New("invalid market data snapshot")
	ErrInvalidUpdate   = errors.New("invalid market data update")
	ErrNotSynced       = errors.New("market data book not synced")
)

type Level struct {
	Price    domain.Money
	Quantity domain.Quantity
}

type Snapshot struct {
	Symbol   domain.Symbol
	Sequence domain.Sequence
	Bids     []Level
	Asks     []Level
}

type Update struct {
	Symbol        domain.Symbol
	FirstSequence domain.Sequence
	Sequence      domain.Sequence
	Bids          []Level
	Asks          []Level
}

type Book struct {
	mu       sync.Mutex
	symbol   domain.Symbol
	sequence domain.Sequence
	synced   bool
	bids     map[domain.Money]domain.Quantity
	asks     map[domain.Money]domain.Quantity
}

func NewBook(symbol domain.Symbol) *Book {
	return &Book{
		symbol: symbol,
		bids:   make(map[domain.Money]domain.Quantity),
		asks:   make(map[domain.Money]domain.Quantity),
	}
}

func (b *Book) ApplySnapshot(snapshot Snapshot) error {
	if snapshot.Symbol == "" || snapshot.Sequence <= 0 || snapshot.Symbol != b.symbol {
		return ErrInvalidSnapshot
	}
	if err := validateLevels(snapshot.Bids); err != nil {
		return err
	}
	if err := validateLevels(snapshot.Asks); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.bids = levelsToMap(snapshot.Bids)
	b.asks = levelsToMap(snapshot.Asks)
	b.sequence = snapshot.Sequence
	b.synced = true
	return nil
}

func (b *Book) ApplyUpdate(update Update) error {
	if update.Symbol == "" || update.Sequence <= 0 || update.Symbol != b.symbol {
		return ErrInvalidUpdate
	}
	if err := validateLevels(update.Bids); err != nil {
		return err
	}
	if err := validateLevels(update.Asks); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.synced {
		return ErrNotSynced
	}
	if !canApplyUpdate(b.sequence, update) {
		b.synced = false
		return ErrGapDetected
	}

	applyLevels(b.bids, update.Bids)
	applyLevels(b.asks, update.Asks)
	b.sequence = update.Sequence
	return nil
}

func canApplyUpdate(current domain.Sequence, update Update) bool {
	if update.FirstSequence == 0 {
		return update.Sequence == current+1
	}
	return update.FirstSequence <= current+1 && update.Sequence > current
}

func (b *Book) Snapshot() Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()

	return Snapshot{
		Symbol:   b.symbol,
		Sequence: b.sequence,
		Bids:     sortedLevels(b.bids, true),
		Asks:     sortedLevels(b.asks, false),
	}
}

func (b *Book) Synced() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.synced
}

func validateLevels(levels []Level) error {
	for _, level := range levels {
		if level.Price <= 0 || level.Quantity < 0 {
			return ErrInvalidUpdate
		}
	}
	return nil
}

func levelsToMap(levels []Level) map[domain.Money]domain.Quantity {
	result := make(map[domain.Money]domain.Quantity, len(levels))
	for _, level := range levels {
		if level.Quantity > 0 {
			result[level.Price] = level.Quantity
		}
	}
	return result
}

func applyLevels(book map[domain.Money]domain.Quantity, levels []Level) {
	for _, level := range levels {
		if level.Quantity == 0 {
			delete(book, level.Price)
			continue
		}
		book[level.Price] = level.Quantity
	}
}

func sortedLevels(levels map[domain.Money]domain.Quantity, descending bool) []Level {
	prices := make([]domain.Money, 0, len(levels))
	for price := range levels {
		prices = append(prices, price)
	}
	sort.Slice(prices, func(i, j int) bool {
		if descending {
			return prices[i] > prices[j]
		}
		return prices[i] < prices[j]
	})

	result := make([]Level, 0, len(prices))
	for _, price := range prices {
		result = append(result, Level{Price: price, Quantity: levels[price]})
	}
	return result
}
