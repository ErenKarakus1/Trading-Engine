package execution

import (
	"errors"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
	"github.com/ErenKarakus1/Trading-Engine/internal/marketdata"
)

var ErrInvalidOrder = errors.New("invalid simulated order")

type Fill struct {
	Price    domain.Money
	Quantity domain.Quantity
	Notional domain.Money
}

type Result struct {
	Symbol            domain.Symbol
	Side              domain.Side
	RequestedQuantity domain.Quantity
	FilledQuantity    domain.Quantity
	RemainingQuantity domain.Quantity
	Notional          domain.Money
	AveragePrice      domain.Money
	BestPrice         domain.Money
	MidPrice          domain.Money
	SlippageBpsVsBest int64
	SlippageBpsVsMid  int64
	Fills             []Fill
}

func SimulateMarketOrder(snapshot marketdata.Snapshot, side domain.Side, quantity domain.Quantity) (Result, error) {
	if quantity <= 0 || (side != domain.SideBuy && side != domain.SideSell) {
		return Result{}, ErrInvalidOrder
	}

	result := Result{
		Symbol:            snapshot.Symbol,
		Side:              side,
		RequestedQuantity: quantity,
	}

	levels := snapshot.Asks
	if side == domain.SideSell {
		levels = snapshot.Bids
	}
	if len(levels) > 0 {
		result.BestPrice = levels[0].Price
	}
	if len(snapshot.Bids) > 0 && len(snapshot.Asks) > 0 {
		result.MidPrice = domain.Money((int64(snapshot.Bids[0].Price) + int64(snapshot.Asks[0].Price)) / 2)
	}

	remaining := quantity
	for _, level := range levels {
		if remaining == 0 {
			break
		}
		fillQuantity := minQuantity(remaining, level.Quantity)
		notional := multiply(level.Price, fillQuantity)
		result.Fills = append(result.Fills, Fill{
			Price:    level.Price,
			Quantity: fillQuantity,
			Notional: notional,
		})
		result.FilledQuantity += fillQuantity
		result.Notional += notional
		remaining -= fillQuantity
	}

	result.RemainingQuantity = remaining
	if result.FilledQuantity > 0 {
		result.AveragePrice = domain.Money(int64(result.Notional) / int64(result.FilledQuantity))
		result.SlippageBpsVsBest = slippageBps(side, result.AveragePrice, result.BestPrice)
		result.SlippageBpsVsMid = slippageBps(side, result.AveragePrice, result.MidPrice)
	}

	return result, nil
}

func minQuantity(a, b domain.Quantity) domain.Quantity {
	if a < b {
		return a
	}
	return b
}

func multiply(price domain.Money, quantity domain.Quantity) domain.Money {
	return domain.Money(int64(price) * int64(quantity))
}

func slippageBps(side domain.Side, executionPrice, referencePrice domain.Money) int64 {
	if executionPrice == 0 || referencePrice == 0 {
		return 0
	}

	diff := int64(executionPrice) - int64(referencePrice)
	if side == domain.SideSell {
		diff = int64(referencePrice) - int64(executionPrice)
	}
	return diff * 10_000 / int64(referencePrice)
}
