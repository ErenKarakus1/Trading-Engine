package api

import (
	"errors"
	"sync"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
	"github.com/ErenKarakus1/Trading-Engine/internal/matching"
	"github.com/ErenKarakus1/Trading-Engine/internal/orderbook"
	"github.com/ErenKarakus1/Trading-Engine/internal/ratelimit"
	"github.com/ErenKarakus1/Trading-Engine/internal/risk"
	"github.com/gin-gonic/gin"
	"net/http"
)

type RateLimiter interface {
	AllowGin(c *gin.Context, subject string) error
}

type RiskChecker interface {
	Check(account risk.Account, order matching.Order) error
}

type Server struct {
	matcher     *matching.Engine
	riskChecker RiskChecker
	limiter     RateLimiter

	mu       sync.Mutex
	accounts map[risk.AccountID]risk.Account
	orders   map[domain.OrderID]orderRecord
	trades   []matching.Trade
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
		accounts:    make(map[risk.AccountID]risk.Account),
		orders:      make(map[domain.OrderID]orderRecord),
	}
	for _, account := range accounts {
		server.accounts[account.ID] = account
	}
	return server
}

func (s *Server) Router() *gin.Engine {
	router := gin.New()
	router.POST("/orders", s.handlePostOrder)
	router.DELETE("/orders/:id", s.handleDeleteOrder)
	router.GET("/orders/:id", s.handleGetOrder)
	router.GET("/orderbook/:symbol", s.handleGetOrderBook)
	router.GET("/trades/:symbol", s.handleGetTrades)
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
		writeError(c, http.StatusBadRequest, "invalid_json")
		return
	}

	account, ok := s.account(risk.AccountID(request.AccountID))
	if !ok {
		writeError(c, http.StatusNotFound, "account_not_found")
		return
	}
	if err := s.allow(c, request.AccountID); err != nil {
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
			writeError(c, http.StatusForbidden, err.Error())
			return
		}
	}

	result, err := s.matcher.Submit(order)
	if err != nil {
		writeError(c, http.StatusBadRequest, err.Error())
		return
	}
	s.recordSubmit(result)

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

	result, err := s.matcher.Cancel(record.Order.Symbol, orderID)
	if err != nil {
		writeError(c, http.StatusNotFound, err.Error())
		return
	}
	s.recordCancel(result)

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

type orderBookResponse struct {
	Symbol  domain.Symbol         `json:"symbol"`
	BestBid *orderbook.PriceLevel `json:"best_bid,omitempty"`
	BestAsk *orderbook.PriceLevel `json:"best_ask,omitempty"`
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
