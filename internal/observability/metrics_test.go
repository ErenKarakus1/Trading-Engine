package observability

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetricsExposeCounters(t *testing.T) {
	metrics := NewMetrics()
	metrics.ObserveHTTPRequest("GET", "/orders/:id", 200, 10*time.Millisecond)
	metrics.OrderAccepted("BTC-USD", "buy")
	metrics.OrderRejected("insufficient balance")
	metrics.TradesExecuted("BTC-USD", 2)
	metrics.ObserveMatching("submit", "BTC-USD", time.Millisecond)
	metrics.WebSocketConnected("BTC-USD")
	metrics.WebSocketMessage("BTC-USD", "events")
	metrics.MarketDataMessage("BTC-USD", "update")
	metrics.MarketDataReconnect("BTC-USD")
	metrics.SetKafkaConsumerLag("engine-consumer", "fills", 3)

	if err := testutil.GatherAndCompare(metrics.Registry(), strings.NewReader(`
# HELP trading_engine_orders_accepted_total Accepted orders by symbol and side.
# TYPE trading_engine_orders_accepted_total counter
trading_engine_orders_accepted_total{side="buy",symbol="BTC-USD"} 1
# HELP trading_engine_orders_rejected_total Rejected orders by reason.
# TYPE trading_engine_orders_rejected_total counter
trading_engine_orders_rejected_total{reason="insufficient balance"} 1
# HELP trading_engine_trades_executed_total Executed trades by symbol.
# TYPE trading_engine_trades_executed_total counter
trading_engine_trades_executed_total{symbol="BTC-USD"} 2
# HELP trading_engine_market_data_messages_total External market-data messages processed by symbol and type.
# TYPE trading_engine_market_data_messages_total counter
trading_engine_market_data_messages_total{symbol="BTC-USD",type="update"} 1
# HELP trading_engine_market_data_reconnects_total External market-data reconnects by symbol.
# TYPE trading_engine_market_data_reconnects_total counter
trading_engine_market_data_reconnects_total{symbol="BTC-USD"} 1
# HELP trading_engine_kafka_consumer_lag Kafka consumer lag by group and topic.
# TYPE trading_engine_kafka_consumer_lag gauge
trading_engine_kafka_consumer_lag{group="engine-consumer",topic="fills"} 3
# HELP trading_engine_websocket_connections Active WebSocket connections by symbol.
# TYPE trading_engine_websocket_connections gauge
trading_engine_websocket_connections{symbol="BTC-USD"} 1
# HELP trading_engine_websocket_messages_total WebSocket messages sent by symbol and type.
# TYPE trading_engine_websocket_messages_total counter
trading_engine_websocket_messages_total{symbol="BTC-USD",type="events"} 1
`),
		"trading_engine_orders_accepted_total",
		"trading_engine_orders_rejected_total",
		"trading_engine_trades_executed_total",
		"trading_engine_market_data_messages_total",
		"trading_engine_market_data_reconnects_total",
		"trading_engine_kafka_consumer_lag",
		"trading_engine_websocket_connections",
		"trading_engine_websocket_messages_total",
	); err != nil {
		t.Fatalf("GatherAndCompare() error = %v", err)
	}
}
