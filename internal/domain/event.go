package domain

type EventType string

const (
	EventTypeOrderAccepted EventType = "order_accepted"
	EventTypeTradeExecuted EventType = "trade_executed"
	EventTypeOrderRested   EventType = "order_rested"
	EventTypeOrderCanceled EventType = "order_canceled"
)
