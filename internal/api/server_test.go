package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
	"github.com/ErenKarakus1/Trading-Engine/internal/matching"
	"github.com/ErenKarakus1/Trading-Engine/internal/orderbook"
	"github.com/ErenKarakus1/Trading-Engine/internal/ratelimit"
	"github.com/ErenKarakus1/Trading-Engine/internal/risk"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestPostOrderSubmitsAcceptedOrder(t *testing.T) {
	server := newTestServer(nil, nil)
	response := postJSON(t, server, "/orders", orderRequest{
		AccountID: "account-1",
		ID:        "buy-1",
		Symbol:    "BTC-USD",
		Side:      domain.SideBuy,
		Type:      domain.OrderTypeLimit,
		Price:     100,
		Quantity:  5,
	})

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusCreated)
	}

	var result matching.Result
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if result.Sequence != 1 || !result.Rested {
		t.Fatalf("result = %+v", result)
	}
}

func TestPostOrderAppliesRisk(t *testing.T) {
	server := newTestServer(&fakeRisk{err: risk.ErrInsufficientBalance}, nil)
	response := postJSON(t, server, "/orders", orderRequest{
		AccountID: "account-1",
		ID:        "buy-1",
		Symbol:    "BTC-USD",
		Side:      domain.SideBuy,
		Type:      domain.OrderTypeLimit,
		Price:     100,
		Quantity:  5,
	})

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestPostOrderAppliesRateLimit(t *testing.T) {
	server := newTestServer(nil, &fakeLimiter{err: ratelimit.ErrLimited})
	response := postJSON(t, server, "/orders", orderRequest{
		AccountID: "account-1",
		ID:        "buy-1",
		Symbol:    "BTC-USD",
		Side:      domain.SideBuy,
		Type:      domain.OrderTypeLimit,
		Price:     100,
		Quantity:  5,
	})

	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusTooManyRequests)
	}
}

func TestGetOrderBook(t *testing.T) {
	server := newTestServer(nil, nil)
	postJSON(t, server, "/orders", orderRequest{
		AccountID: "account-1",
		ID:        "buy-1",
		Symbol:    "BTC-USD",
		Side:      domain.SideBuy,
		Type:      domain.OrderTypeLimit,
		Price:     100,
		Quantity:  5,
	})

	request := httptest.NewRequest(http.MethodGet, "/orderbook/BTC-USD", nil)
	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var book orderBookResponse
	if err := json.NewDecoder(response.Body).Decode(&book); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if book.BestBid == nil || book.BestBid.Price != 100 {
		t.Fatalf("BestBid = %+v, want price 100", book.BestBid)
	}
}

func TestSeedOrdersPopulatesBookAndTrades(t *testing.T) {
	server := newTestServer(nil, nil)
	err := server.SeedOrders([]matching.Order{
		{ID: "seed-buy-1", Symbol: "BTC-USD", Side: domain.SideBuy, Type: domain.OrderTypeLimit, Price: 100, Quantity: 10},
		{ID: "seed-sell-1", Symbol: "BTC-USD", Side: domain.SideSell, Type: domain.OrderTypeLimit, Price: 102, Quantity: 8},
		{ID: "seed-sell-2", Symbol: "BTC-USD", Side: domain.SideSell, Type: domain.OrderTypeLimit, Price: 100, Quantity: 3},
	})
	if err != nil {
		t.Fatalf("SeedOrders() error = %v", err)
	}

	bookResponse := httptest.NewRecorder()
	server.Router().ServeHTTP(bookResponse, httptest.NewRequest(http.MethodGet, "/orderbook/BTC-USD", nil))
	var book orderBookResponse
	if err := json.NewDecoder(bookResponse.Body).Decode(&book); err != nil {
		t.Fatalf("Decode(book) error = %v", err)
	}
	if book.BestBid == nil || book.BestBid.Price != 100 || book.BestBid.Quantity != 7 {
		t.Fatalf("BestBid = %+v, want price 100 quantity 7", book.BestBid)
	}
	if book.BestAsk == nil || book.BestAsk.Price != 102 || book.BestAsk.Quantity != 8 {
		t.Fatalf("BestAsk = %+v, want price 102 quantity 8", book.BestAsk)
	}

	tradesResponse := httptest.NewRecorder()
	server.Router().ServeHTTP(tradesResponse, httptest.NewRequest(http.MethodGet, "/trades/BTC-USD", nil))
	var trades []matching.Trade
	if err := json.NewDecoder(tradesResponse.Body).Decode(&trades); err != nil {
		t.Fatalf("Decode(trades) error = %v", err)
	}
	if len(trades) != 1 || trades[0].Quantity != 3 {
		t.Fatalf("trades = %+v, want one seeded trade quantity 3", trades)
	}
}

func TestRestoreRebuildsBookAndTrades(t *testing.T) {
	server := newTestServer(nil, nil)
	snapshot := matching.Snapshot{
		Symbol:   "BTC-USD",
		Sequence: 1,
		Orders: []orderbook.Order{
			{ID: "resting-buy", Side: domain.SideBuy, Price: 100, Quantity: 5},
		},
	}
	events := []matching.Event{
		{
			Type:     domain.EventTypeOrderAccepted,
			Sequence: 2,
			Order:    &matching.Order{ID: "sell-1", Symbol: "BTC-USD", Side: domain.SideSell, Type: domain.OrderTypeLimit, Price: 100, Quantity: 2},
		},
		{
			Type:     domain.EventTypeTradeExecuted,
			Sequence: 2,
			Trade:    &matching.Trade{Sequence: 2, Symbol: "BTC-USD", MakerOrderID: "resting-buy", TakerOrderID: "sell-1", Price: 100, Quantity: 2},
		},
	}

	if err := server.Restore(snapshot, events, nil); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}

	bookResponse := httptest.NewRecorder()
	server.Router().ServeHTTP(bookResponse, httptest.NewRequest(http.MethodGet, "/orderbook/BTC-USD", nil))
	var book orderBookResponse
	if err := json.NewDecoder(bookResponse.Body).Decode(&book); err != nil {
		t.Fatalf("Decode(book) error = %v", err)
	}
	if book.BestBid == nil || book.BestBid.Quantity != 3 {
		t.Fatalf("BestBid = %+v, want quantity 3", book.BestBid)
	}

	tradesResponse := httptest.NewRecorder()
	server.Router().ServeHTTP(tradesResponse, httptest.NewRequest(http.MethodGet, "/trades/BTC-USD", nil))
	var trades []matching.Trade
	if err := json.NewDecoder(tradesResponse.Body).Decode(&trades); err != nil {
		t.Fatalf("Decode(trades) error = %v", err)
	}
	if len(trades) != 1 || trades[0].Quantity != 2 {
		t.Fatalf("trades = %+v, want one restored trade quantity 2", trades)
	}
}

func TestRestoreKeepsTradeTapeWhenSnapshotIsCurrent(t *testing.T) {
	server := newTestServer(nil, nil)
	snapshot := matching.Snapshot{
		Symbol:   "BTC-USD",
		Sequence: 2,
		Orders: []orderbook.Order{
			{ID: "resting-buy", Side: domain.SideBuy, Price: 100, Quantity: 3},
		},
	}
	trades := []matching.Trade{
		{Sequence: 2, Symbol: "BTC-USD", MakerOrderID: "resting-buy", TakerOrderID: "sell-1", Price: 100, Quantity: 2},
	}

	if err := server.Restore(snapshot, nil, trades); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}

	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/trades/BTC-USD", nil))
	var restored []matching.Trade
	if err := json.NewDecoder(response.Body).Decode(&restored); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(restored) != 1 || restored[0].Quantity != 2 {
		t.Fatalf("trades = %+v, want restored trade tape", restored)
	}
}

func TestDeleteOrderCancelsRestingOrder(t *testing.T) {
	server := newTestServer(nil, nil)
	postJSON(t, server, "/orders", orderRequest{
		AccountID: "account-1",
		ID:        "buy-1",
		Symbol:    "BTC-USD",
		Side:      domain.SideBuy,
		Type:      domain.OrderTypeLimit,
		Price:     100,
		Quantity:  5,
	})

	request := httptest.NewRequest(http.MethodDelete, "/orders/buy-1", nil)
	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}

	getResponse := httptest.NewRecorder()
	server.Router().ServeHTTP(getResponse, httptest.NewRequest(http.MethodGet, "/orders/buy-1", nil))
	var record orderRecord
	if err := json.NewDecoder(getResponse.Body).Decode(&record); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if record.Status != "canceled" {
		t.Fatalf("Status = %q, want canceled", record.Status)
	}
}

func TestGetTrades(t *testing.T) {
	server := newTestServer(nil, nil)
	postJSON(t, server, "/orders", orderRequest{AccountID: "account-1", ID: "sell-1", Symbol: "BTC-USD", Side: domain.SideSell, Type: domain.OrderTypeLimit, Price: 100, Quantity: 5})
	postJSON(t, server, "/orders", orderRequest{AccountID: "account-1", ID: "buy-1", Symbol: "BTC-USD", Side: domain.SideBuy, Type: domain.OrderTypeLimit, Price: 100, Quantity: 3})

	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/trades/BTC-USD", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var trades []matching.Trade
	if err := json.NewDecoder(response.Body).Decode(&trades); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(trades) != 1 || trades[0].Quantity != 3 {
		t.Fatalf("trades = %+v, want one trade quantity 3", trades)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	server := newTestServer(nil, nil)
	postJSON(t, server, "/orders", orderRequest{
		AccountID: "account-1",
		ID:        "buy-1",
		Symbol:    "BTC-USD",
		Side:      domain.SideBuy,
		Type:      domain.OrderTypeLimit,
		Price:     100,
		Quantity:  5,
	})

	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	body := response.Body.String()
	for _, metric := range []string{
		"trading_engine_http_requests_total",
		"trading_engine_orders_accepted_total",
	} {
		if !strings.Contains(body, metric) {
			t.Fatalf("metrics body does not contain %q", metric)
		}
	}
}

func TestOrderBookWebSocketReceivesSnapshotAndEvents(t *testing.T) {
	server := newTestServer(nil, nil)
	httpServer := httptest.NewServer(server.Router())
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws/orderbook/BTC-USD"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	var snapshot websocketMessage
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatalf("ReadJSON(snapshot) error = %v", err)
	}
	if snapshot.Type != "snapshot" || snapshot.Symbol != "BTC-USD" || snapshot.Book == nil {
		t.Fatalf("snapshot = %+v", snapshot)
	}

	response := postJSON(t, server, "/orders", orderRequest{
		AccountID: "account-1",
		ID:        "buy-1",
		Symbol:    "BTC-USD",
		Side:      domain.SideBuy,
		Type:      domain.OrderTypeLimit,
		Price:     100,
		Quantity:  5,
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusCreated)
	}

	var message websocketMessage
	if err := conn.ReadJSON(&message); err != nil {
		t.Fatalf("ReadJSON(events) error = %v", err)
	}
	if message.Type != "events" || message.Symbol != "BTC-USD" {
		t.Fatalf("message = %+v", message)
	}
	if len(message.Events) != 2 {
		t.Fatalf("len(Events) = %d, want 2", len(message.Events))
	}
}

func newTestServer(riskChecker RiskChecker, limiter RateLimiter) *Server {
	if riskChecker == nil {
		riskChecker = &fakeRisk{}
	}
	return NewServer(matching.NewEngine(), riskChecker, limiter, []risk.Account{
		{ID: "account-1", Cash: 1_000, Positions: map[domain.Symbol]domain.Quantity{"BTC-USD": 100}},
	})
}

func postJSON(t *testing.T, server *Server, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, request)
	return response
}

type fakeRisk struct {
	err error
}

func (r *fakeRisk) Check(risk.Account, matching.Order) error {
	return r.err
}

type fakeLimiter struct {
	err error
}

func (l *fakeLimiter) AllowGin(*gin.Context, string) error {
	if l.err != nil {
		return l.err
	}
	return nil
}

func TestPostOrderReturnsAccountNotFound(t *testing.T) {
	server := newTestServer(nil, nil)
	response := postJSON(t, server, "/orders", orderRequest{AccountID: "missing"})

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestPostOrderReturnsLimiterFailure(t *testing.T) {
	server := newTestServer(nil, &fakeLimiter{err: errors.New("redis down")})
	response := postJSON(t, server, "/orders", orderRequest{AccountID: "account-1"})

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}
