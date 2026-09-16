package domain

type OrderID string

type Quantity int64

type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)
