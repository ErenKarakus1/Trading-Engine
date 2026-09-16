package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
	"github.com/ErenKarakus1/Trading-Engine/internal/matching"
	"github.com/ErenKarakus1/Trading-Engine/internal/observability"
	"github.com/ErenKarakus1/Trading-Engine/internal/orderbook"
	"github.com/ErenKarakus1/Trading-Engine/internal/ratelimit"
	"github.com/ErenKarakus1/Trading-Engine/internal/risk"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type RateLimiter interface {
	AllowGin(c *gin.Context, subject string) error
}

type RiskChecker interface {
	Check(account risk.Account, order matching.Order) error
}

type EventStore interface {
	SaveEvents(context.Context, []matching.Event) error
}

type EventPublisher interface {
	PublishEvents(context.Context, []matching.Event) error
}

type Server struct {
	matcher     *matching.Engine
	riskChecker RiskChecker
	limiter     RateLimiter
	upgrader    websocket.Upgrader
	metrics     *observability.Metrics
	eventStore  EventStore
	publisher   EventPublisher

	mu       sync.Mutex
	accounts map[risk.AccountID]risk.Account
	orders   map[domain.OrderID]orderRecord
	trades   []matching.Trade
	clients  map[domain.Symbol]map[*websocket.Conn]struct{}
}

type orderRecord struct {
	Order     matching.Order  `json:"order"`
	Sequence  domain.Sequence `json:"sequence"`
	Remaining domain.Quantity `json:"remaining"`
	Status    string          `json:"status"`
}

func NewServer(matcher *matching.Engine, riskChecker RiskChecker, limiter RateLimiter, accounts []risk.Account) *Server {
	if matcher == nil {
		matcher = matching.NewEngine()
	}

	server := &Server{
		matcher:     matcher,
		riskChecker: riskChecker,
		limiter:     limiter,
		upgrader:    websocket.Upgrader{},
		metrics:     observability.NewMetrics(),
		accounts:    make(map[risk.AccountID]risk.Account),
		orders:      make(map[domain.OrderID]orderRecord),
		clients:     make(map[domain.Symbol]map[*websocket.Conn]struct{}),
	}
	for _, account := range accounts {
		server.accounts[account.ID] = account
	}
	return server
}

func (s *Server) UseEventStore(store EventStore) {
	s.eventStore = store
}

func (s *Server) UseEventPublisher(publisher EventPublisher) {
	s.publisher = publisher
}

func (s *Server) Router() *gin.Engine {
	router := gin.New()
	router.Use(s.metricsMiddleware())
	router.GET("/metrics", gin.WrapH(promhttp.HandlerFor(s.metrics.Registry(), promhttp.HandlerOpts{})))
	router.POST("/orders", s.handlePostOrder)
	router.DELETE("/orders/:id", s.handleDeleteOrder)
	router.GET("/orders/:id", s.handleGetOrder)
	router.GET("/orderbook/:symbol", s.handleGetOrderBook)
	router.GET("/trades/:symbol", s.handleGetTrades)
	router.GET("/ws/orderbook/:symbol", s.handleOrderBookSocket)
	return router
}

type orderRequest struct {
	AccountID string           `json:"account_id"`
	ID        domain.OrderID   `json:"id"`
	Symbol    domain.Symbol    `json:"symbol"`
	Side      domain.Side      `json:"side"`
	Type      domain.OrderType `json:"type"`
	Price     domain.Money     `json:"price"`
	Quantity  domain.Quantity  `json:"quantity"`
}

func (s *Server) handlePostOrder(c *gin.Context) {
	var request orderRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		s.metrics.OrderRejected("invalid_json")
		writeError(c, http.StatusBadRequest, "invalid_json")
		return
	}

	account, ok := s.account(risk.AccountID(request.AccountID))
	if !ok {
		s.metrics.OrderRejected("account_not_found")
		writeError(c, http.StatusNotFound, "account_not_found")
		return
	}
	if err := s.allow(c, request.AccountID); err != nil {
		s.metrics.OrderRejected(err.Error())
		writeRateLimitError(c, err)
		return
	}

	order := matching.Order{
		ID:       request.ID,
		Symbol:   request.Symbol,
		Side:     request.Side,
		Type:     request.Type,
		Price:    request.Price,
		Quantity: request.Quantity,
	}
	if s.riskChecker != nil {
		if err := s.riskChecker.Check(account, order); err != nil {
			s.metrics.OrderRejected(err.Error())
			writeError(c, http.StatusForbidden, err.Error())
			return
		}
	}

	matchStart := time.Now()
	result, err := s.matcher.Submit(order)
	s.metrics.ObserveMatching("submit", string(order.Symbol), time.Since(matchStart))
	if err != nil {
		s.metrics.OrderRejected(err.Error())
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	s.recordSubmit(result)
	s.metrics.OrderAccepted(string(result.Accepted.Symbol), string(result.Accepted.Side))
	s.metrics.TradesExecuted(string(result.Accepted.Symbol), len(result.Trades))
	s.persistAndPublish(result.Events)
	s.broadcast(result.Accepted.Symbol, websocketMessage{
		Type:   "events",
		Events: result.Events,
	})

	c.JSON(http.StatusCreated, result)
}

func (s *Server) handleDeleteOrder(c *gin.Context) {
	orderID := domain.OrderID(c.Param("id"))
	if orderID == "" {
		writeError(c, http.StatusBadRequest, "missing_order_id")
		return
	}

	record, ok := s.order(orderID)
	if !ok {
		writeError(c, http.StatusNotFound, "order_not_found")
		return
	}

	matchStart := time.Now()
	result, err := s.matcher.Cancel(record.Order.Symbol, orderID)
	s.metrics.ObserveMatching("cancel", string(record.Order.Symbol), time.Since(matchStart))
	if err != nil {
		writeError(c, http.StatusNotFound, err.Error())
		return
	}
	s.recordCancel(result)
	s.persistAndPublish([]matching.Event{result.Event})
	s.broadcast(record.Order.Symbol, websocketMessage{
		Type:   "events",
		Events: []matching.Event{result.Event},
	})

	c.JSON(http.StatusOK, result)
}

func (s *Server) handleGetOrder(c *gin.Context) {
	orderID := domain.OrderID(c.Param("id"))
	record, ok := s.order(orderID)
	if !ok {
		writeError(c, http.StatusNotFound, "order_not_found")
		return
	}
	c.JSON(http.StatusOK, record)
}

func (s *Server) handleGetOrderBook(c *gin.Context) {
	symbol := domain.Symbol(c.Param("symbol"))
	if symbol == "" {
		writeError(c, http.StatusBadRequest, "missing_symbol")
		return
	}

	response := orderBookResponse{Symbol: symbol}
	if bid, ok := s.matcher.BestBid(symbol); ok {
		response.BestBid = &bid
	}
	if ask, ok := s.matcher.BestAsk(symbol); ok {
		response.BestAsk = &ask
	}
	c.JSON(http.StatusOK, response)
}

func (s *Server) handleGetTrades(c *gin.Context) {
	symbol := domain.Symbol(c.Param("symbol"))
	if symbol == "" {
		writeError(c, http.StatusBadRequest, "missing_symbol")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	trades := make([]matching.Trade, 0)
	for _, trade := range s.trades {
		if trade.Symbol == symbol {
			trades = append(trades, trade)
		}
	}
	c.JSON(http.StatusOK, trades)
}

func (s *Server) handleOrderBookSocket(c *gin.Context) {
	symbol := domain.Symbol(c.Param("symbol"))
	if symbol == "" {
		writeError(c, http.StatusBadRequest, "missing_symbol")
		return
	}

	conn, err := s.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	s.addClient(symbol, conn)
	s.metrics.WebSocketConnected(string(symbol))
	defer s.removeClient(symbol, conn)
	defer s.metrics.WebSocketDisconnected(string(symbol))
	defer conn.Close()

	if err := conn.WriteJSON(s.orderBookSnapshot(symbol)); err == nil {
		s.metrics.WebSocketMessage(string(symbol), "snapshot")
	}
	for {
		if _, _, err := conn.NextReader(); err != nil {
			return
		}
	}
}

type orderBookResponse struct {
	Symbol  domain.Symbol         `json:"symbol"`
	BestBid *orderbook.PriceLevel `json:"best_bid,omitempty"`
	BestAsk *orderbook.PriceLevel `json:"best_ask,omitempty"`
}

type websocketMessage struct {
	Type   string             `json:"type"`
	Symbol domain.Symbol      `json:"symbol,omitempty"`
	Book   *orderBookResponse `json:"book,omitempty"`
	Events []matching.Event   `json:"events,omitempty"`
}

func (s *Server) account(id risk.AccountID) (risk.Account, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	account, ok := s.accounts[id]
	return account, ok
}

func (s *Server) order(id domain.OrderID) (orderRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.orders[id]
	return record, ok
}

func (s *Server) allow(c *gin.Context, subject string) error {
	if s.limiter == nil {
		return nil
	}
	return s.limiter.AllowGin(c, subject)
}

func (s *Server) recordSubmit(result matching.Result) {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := "filled"
	if result.Rested {
		status = "resting"
	} else if result.Remaining > 0 {
		status = "partially_filled"
	}

	s.orders[result.Accepted.ID] = orderRecord{
		Order:     result.Accepted,
		Sequence:  result.Sequence,
		Remaining: result.Remaining,
		Status:    status,
	}
	for _, trade := range result.Trades {
		s.trades = append(s.trades, trade)
		if maker, ok := s.orders[trade.MakerOrderID]; ok {
			maker.Remaining -= trade.Quantity
			if maker.Remaining <= 0 {
				maker.Remaining = 0
				maker.Status = "filled"
			} else {
				maker.Status = "partially_filled"
			}
			maker.Sequence = result.Sequence
			s.orders[trade.MakerOrderID] = maker
		}
	}
}

func (s *Server) recordCancel(result matching.CancelResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.orders[result.Order.ID]
	if !ok {
		return
	}
	record.Sequence = result.Sequence
	record.Remaining = 0
	record.Status = "canceled"
	s.orders[result.Order.ID] = record
}

func (s *Server) orderBookSnapshot(symbol domain.Symbol) websocketMessage {
	book := orderBookResponse{Symbol: symbol}
	if bid, ok := s.matcher.BestBid(symbol); ok {
		book.BestBid = &bid
	}
	if ask, ok := s.matcher.BestAsk(symbol); ok {
		book.BestAsk = &ask
	}
	return websocketMessage{
		Type:   "snapshot",
		Symbol: symbol,
		Book:   &book,
	}
}

func (s *Server) addClient(symbol domain.Symbol, conn *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.clients[symbol] == nil {
		s.clients[symbol] = make(map[*websocket.Conn]struct{})
	}
	s.clients[symbol][conn] = struct{}{}
}

func (s *Server) removeClient(symbol domain.Symbol, conn *websocket.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.clients[symbol], conn)
	if len(s.clients[symbol]) == 0 {
		delete(s.clients, symbol)
	}
}

func (s *Server) broadcast(symbol domain.Symbol, message websocketMessage) {
	message.Symbol = symbol

	s.mu.Lock()
	clients := make([]*websocket.Conn, 0, len(s.clients[symbol]))
	for client := range s.clients[symbol] {
		clients = append(clients, client)
	}
	s.mu.Unlock()

	for _, client := range clients {
		if err := client.WriteJSON(message); err != nil {
			s.removeClient(symbol, client)
			_ = client.Close()
			continue
		}
		s.metrics.WebSocketMessage(string(symbol), message.Type)
	}
}

func (s *Server) persistAndPublish(events []matching.Event) {
	if len(events) == 0 || (s.eventStore == nil && s.publisher == nil) {
		return
	}

	eventsCopy := append([]matching.Event(nil), events...)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if s.eventStore != nil {
			if err := s.eventStore.SaveEvents(ctx, eventsCopy); err != nil {
				log.Printf("save events: %v", err)
			}
		}
		if s.publisher != nil {
			if err := s.publisher.PublishEvents(ctx, eventsCopy); err != nil {
				log.Printf("publish events: %v", err)
			}
		}
	}()
}

func (s *Server) metricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		s.metrics.ObserveHTTPRequest(c.Request.Method, path, c.Writer.Status(), time.Since(start))
	}
}

func writeRateLimitError(c *gin.Context, err error) {
	if errors.Is(err, ratelimit.ErrLimited) {
		writeError(c, http.StatusTooManyRequests, err.Error())
		return
	}
	writeError(c, http.StatusInternalServerError, err.Error())
}

func writeError(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": message})
}
