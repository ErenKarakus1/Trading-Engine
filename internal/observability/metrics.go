package observability

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	registry             *prometheus.Registry
	httpRequests         *prometheus.CounterVec
	httpRequestDuration  *prometheus.HistogramVec
	ordersAccepted       *prometheus.CounterVec
	ordersRejected       *prometheus.CounterVec
	tradesExecuted       *prometheus.CounterVec
	matchingLatency      *prometheus.HistogramVec
	websocketConnections *prometheus.GaugeVec
	websocketMessages    *prometheus.CounterVec
	marketDataMessages   *prometheus.CounterVec
	marketDataReconnects *prometheus.CounterVec
	kafkaConsumerLag     *prometheus.GaugeVec
}

func NewMetrics() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "trading_engine_http_requests_total",
			Help: "Total HTTP requests handled by the API.",
		}, []string{"method", "path", "status"}),
		httpRequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "trading_engine_http_request_duration_seconds",
			Help:    "HTTP request duration by method, path, and status.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "path", "status"}),
		ordersAccepted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "trading_engine_orders_accepted_total",
			Help: "Accepted orders by symbol and side.",
		}, []string{"symbol", "side"}),
		ordersRejected: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "trading_engine_orders_rejected_total",
			Help: "Rejected orders by reason.",
		}, []string{"reason"}),
		tradesExecuted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "trading_engine_trades_executed_total",
			Help: "Executed trades by symbol.",
		}, []string{"symbol"}),
		matchingLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "trading_engine_matching_latency_seconds",
			Help:    "Time spent in matching engine operations.",
			Buckets: prometheus.DefBuckets,
		}, []string{"operation", "symbol"}),
		websocketConnections: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "trading_engine_websocket_connections",
			Help: "Active WebSocket connections by symbol.",
		}, []string{"symbol"}),
		websocketMessages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "trading_engine_websocket_messages_total",
			Help: "WebSocket messages sent by symbol and type.",
		}, []string{"symbol", "type"}),
		marketDataMessages: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "trading_engine_market_data_messages_total",
			Help: "External market-data messages processed by symbol and type.",
		}, []string{"symbol", "type"}),
		marketDataReconnects: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "trading_engine_market_data_reconnects_total",
			Help: "External market-data reconnects by symbol.",
		}, []string{"symbol"}),
		kafkaConsumerLag: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "trading_engine_kafka_consumer_lag",
			Help: "Kafka consumer lag by group and topic.",
		}, []string{"group", "topic"}),
	}

	m.registry.MustRegister(
		m.httpRequests,
		m.httpRequestDuration,
		m.ordersAccepted,
		m.ordersRejected,
		m.tradesExecuted,
		m.matchingLatency,
		m.websocketConnections,
		m.websocketMessages,
		m.marketDataMessages,
		m.marketDataReconnects,
		m.kafkaConsumerLag,
	)
	return m
}

func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}

func (m *Metrics) ObserveHTTPRequest(method, path string, status int, duration time.Duration) {
	statusLabel := strconv.Itoa(status)
	m.httpRequests.WithLabelValues(method, path, statusLabel).Inc()
	m.httpRequestDuration.WithLabelValues(method, path, statusLabel).Observe(duration.Seconds())
}

func (m *Metrics) OrderAccepted(symbol, side string) {
	m.ordersAccepted.WithLabelValues(symbol, side).Inc()
}

func (m *Metrics) OrderRejected(reason string) {
	m.ordersRejected.WithLabelValues(reason).Inc()
}

func (m *Metrics) TradesExecuted(symbol string, count int) {
	m.tradesExecuted.WithLabelValues(symbol).Add(float64(count))
}

func (m *Metrics) ObserveMatching(operation, symbol string, duration time.Duration) {
	m.matchingLatency.WithLabelValues(operation, symbol).Observe(duration.Seconds())
}

func (m *Metrics) WebSocketConnected(symbol string) {
	m.websocketConnections.WithLabelValues(symbol).Inc()
}

func (m *Metrics) WebSocketDisconnected(symbol string) {
	m.websocketConnections.WithLabelValues(symbol).Dec()
}

func (m *Metrics) WebSocketMessage(symbol, messageType string) {
	m.websocketMessages.WithLabelValues(symbol, messageType).Inc()
}

func (m *Metrics) MarketDataMessage(symbol, messageType string) {
	m.marketDataMessages.WithLabelValues(symbol, messageType).Inc()
}

func (m *Metrics) MarketDataReconnect(symbol string) {
	m.marketDataReconnects.WithLabelValues(symbol).Inc()
}

func (m *Metrics) SetKafkaConsumerLag(group, topic string, lag float64) {
	m.kafkaConsumerLag.WithLabelValues(group, topic).Set(lag)
}
