package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
	"github.com/ErenKarakus1/Trading-Engine/internal/marketdata"
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
	SaveSnapshot(context.Context, matching.Snapshot) error
	EventsBySymbol(context.Context, domain.Symbol, int) ([]matching.Event, error)
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

	mu          sync.Mutex
	accounts    map[risk.AccountID]risk.Account
	orders      map[domain.OrderID]orderRecord
	events      []matching.Event
	trades      []matching.Trade
	clients     map[domain.Symbol]map[*websocket.Conn]struct{}
	marketBooks map[domain.Symbol]*marketdata.Book
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
		upgrader: websocket.Upgrader{
			CheckOrigin: sameMachineOrigin,
		},
		metrics:     observability.NewMetrics(),
		accounts:    make(map[risk.AccountID]risk.Account),
		orders:      make(map[domain.OrderID]orderRecord),
		clients:     make(map[domain.Symbol]map[*websocket.Conn]struct{}),
		marketBooks: make(map[domain.Symbol]*marketdata.Book),
	}
	for _, account := range accounts {
		server.accounts[account.ID] = account
	}
	return server
}

func sameMachineOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return strings.HasPrefix(origin, "http://127.0.0.1:") ||
		strings.HasPrefix(origin, "http://localhost:") ||
		strings.HasPrefix(origin, "https://127.0.0.1:") ||
		strings.HasPrefix(origin, "https://localhost:")
}

func (s *Server) SeedOrders(orders []matching.Order) error {
	for _, order := range orders {
		result, err := s.matcher.Submit(order)
		if err != nil {
			return err
		}
		s.recordSubmit(result)
	}
	return nil
}

func (s *Server) Restore(snapshot matching.Snapshot, events []matching.Event, trades []matching.Trade) error {
	if snapshot.Symbol != "" {
		if err := s.matcher.Restore(snapshot); err != nil {
			return err
		}
		s.restoreSnapshotRecords(snapshot)
	}
	if len(events) > 0 {
		if err := s.matcher.Replay(events); err != nil {
			return err
		}
		for _, event := range events {
			s.recordEvent(event)
		}
	}
	s.restoreTrades(trades)
	return nil
}

func (s *Server) UseEventStore(store EventStore) {
	s.eventStore = store
}

func (s *Server) UseEventPublisher(publisher EventPublisher) {
	s.publisher = publisher
}

func (s *Server) UseMarketDataBook(symbol domain.Symbol, book *marketdata.Book) {
	if symbol == "" || book == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.marketBooks[symbol] = book
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
	router.GET("/events/:symbol", s.handleGetEvents)
	router.GET("/marketdata/:symbol", s.handleGetMarketData)
	router.GET("/ws/orderbook/:symbol", s.handleOrderBookSocket)
	router.GET("/ws/marketdata/:symbol", s.handleMarketDataSocket)
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

func (s *Server) handleGetEvents(c *gin.Context) {
	symbol := domain.Symbol(c.Param("symbol"))
	if symbol == "" {
		writeError(c, http.StatusBadRequest, "missing_symbol")
		return
	}

	limit := 120
	if rawLimit := c.Query("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 {
			writeError(c, http.StatusBadRequest, "invalid_limit")
			return
		}
		limit = parsed
	}
	if limit > 500 {
		limit = 500
	}

	if s.eventStore != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		events, err := s.eventStore.EventsBySymbol(ctx, symbol, limit)
		if err != nil {
			log.Printf("load events: %v", err)
			writeError(c, http.StatusInternalServerError, "events_unavailable")
			return
		}
		c.JSON(http.StatusOK, events)
		return
	}

	c.JSON(http.StatusOK, s.recentEvents(symbol, limit))
}

func (s *Server) handleGetMarketData(c *gin.Context) {
	snapshot, ok := s.marketDataSnapshot(domain.Symbol(c.Param("symbol")))
	if !ok {
		writeError(c, http.StatusNotFound, "market_data_not_found")
		return
	}
	c.JSON(http.StatusOK, snapshot)
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

func (s *Server) handleMarketDataSocket(c *gin.Context) {
	symbol := domain.Symbol(c.Param("symbol"))
	if symbol == "" {
		writeError(c, http.StatusBadRequest, "missing_symbol")
		return
	}

	conn, err := s.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		snapshot, ok := s.marketDataSnapshot(symbol)
		if ok {
			if err := conn.WriteJSON(marketDataMessage{Type: "snapshot", Symbol: symbol, Snapshot: &snapshot}); err != nil {
				return
			}
		}
		<-ticker.C
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

type marketDataMessage struct {
	Type     string               `json:"type"`
	Symbol   domain.Symbol        `json:"symbol"`
	Snapshot *marketdata.Snapshot `json:"snapshot,omitempty"`
}

func (s *Server) marketDataSnapshot(symbol domain.Symbol) (marketdata.Snapshot, bool) {
	s.mu.Lock()
	book, ok := s.marketBooks[symbol]
	s.mu.Unlock()
	if !ok {
		return marketdata.Snapshot{}, false
	}
	return book.Snapshot(), true
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
	s.events = append(s.events, result.Events...)
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
	s.events = append(s.events, result.Event)
}

func (s *Server) restoreSnapshotRecords(snapshot matching.Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, order := range snapshot.Orders {
		s.orders[order.ID] = orderRecord{
			Order: matching.Order{
				ID:       order.ID,
				Symbol:   snapshot.Symbol,
				Side:     order.Side,
				Type:     domain.OrderTypeLimit,
				Price:    order.Price,
				Quantity: order.Quantity,
			},
			Sequence:  snapshot.Sequence,
			Remaining: order.Quantity,
			Status:    "resting",
		}
	}
}

func (s *Server) restoreTrades(trades []matching.Trade) {
	if len(trades) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.trades = append([]matching.Trade(nil), trades...)
}

func (s *Server) recordEvent(event matching.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, event)
	switch event.Type {
	case domain.EventTypeOrderAccepted:
		if event.Order != nil {
			s.orders[event.Order.ID] = orderRecord{
				Order:     *event.Order,
				Sequence:  event.Sequence,
				Remaining: event.Order.Quantity,
				Status:    "accepted",
			}
		}
	case domain.EventTypeOrderRested:
		if event.Order != nil {
			record := s.orders[event.Order.ID]
			record.Order = *event.Order
			record.Sequence = event.Sequence
			record.Remaining = event.Order.Quantity
			record.Status = "resting"
			s.orders[event.Order.ID] = record
		}
	case domain.EventTypeTradeExecuted:
		if event.Trade != nil {
			s.trades = append(s.trades, *event.Trade)
			s.reduceRecordedOrder(event.Trade.MakerOrderID, event.Trade.Quantity, event.Sequence)
			s.reduceRecordedOrder(event.Trade.TakerOrderID, event.Trade.Quantity, event.Sequence)
		}
	case domain.EventTypeOrderCanceled:
		if event.Cancel != nil {
			record := s.orders[event.Cancel.ID]
			record.Sequence = event.Sequence
			record.Remaining = 0
			record.Status = "canceled"
			s.orders[event.Cancel.ID] = record
		}
	}
}

func (s *Server) recentEvents(symbol domain.Symbol, limit int) []matching.Event {
	s.mu.Lock()
	defer s.mu.Unlock()

	events := make([]matching.Event, 0, limit)
	for i := len(s.events) - 1; i >= 0 && len(events) < limit; i-- {
		if eventSymbol(s.events[i]) == symbol {
			events = append(events, s.events[i])
		}
	}
	return events
}

func (s *Server) reduceRecordedOrder(orderID domain.OrderID, quantity domain.Quantity, sequence domain.Sequence) {
	record, ok := s.orders[orderID]
	if !ok {
		return
	}
	record.Remaining -= quantity
	if record.Remaining <= 0 {
		record.Remaining = 0
		record.Status = "filled"
	} else {
		record.Status = "partially_filled"
	}
	record.Sequence = sequence
	s.orders[orderID] = record
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
	if s.eventStore != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := s.eventStore.SaveEvents(ctx, eventsCopy); err != nil {
			log.Printf("save events: %v", err)
		}
		for _, symbol := range eventSymbols(eventsCopy) {
			if err := s.eventStore.SaveSnapshot(ctx, s.matcher.Snapshot(symbol)); err != nil {
				log.Printf("save snapshot: %v", err)
			}
		}
	}

	if s.publisher != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if err := s.publisher.PublishEvents(ctx, eventsCopy); err != nil {
				log.Printf("publish events: %v", err)
			}
		}()
	}
}

func eventSymbols(events []matching.Event) []domain.Symbol {
	seen := make(map[domain.Symbol]struct{})
	symbols := make([]domain.Symbol, 0)
	for _, event := range events {
		var symbol domain.Symbol
		switch {
		case event.Order != nil:
			symbol = event.Order.Symbol
		case event.Trade != nil:
			symbol = event.Trade.Symbol
		case event.Cancel != nil:
			symbol = event.Cancel.Symbol
		}
		if symbol == "" {
			continue
		}
		if _, ok := seen[symbol]; ok {
			continue
		}
		seen[symbol] = struct{}{}
		symbols = append(symbols, symbol)
	}
	return symbols
}

func eventSymbol(event matching.Event) domain.Symbol {
	switch {
	case event.Order != nil:
		return event.Order.Symbol
	case event.Trade != nil:
		return event.Trade.Symbol
	case event.Cancel != nil:
		return event.Cancel.Symbol
	default:
		return ""
	}
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
