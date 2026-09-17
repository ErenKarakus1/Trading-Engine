package app

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ErenKarakus1/Trading-Engine/internal/api"
	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
	"github.com/ErenKarakus1/Trading-Engine/internal/kafka"
	"github.com/ErenKarakus1/Trading-Engine/internal/marketdata"
	"github.com/ErenKarakus1/Trading-Engine/internal/matching"
	"github.com/ErenKarakus1/Trading-Engine/internal/postgres"
	"github.com/ErenKarakus1/Trading-Engine/internal/ratelimit"
	"github.com/ErenKarakus1/Trading-Engine/internal/risk"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Run is the application entrypoint.
func Run() error {
	addr := os.Getenv("TRADING_ENGINE_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var limiter api.RateLimiter
	redisAddr := getenv("REDIS_ADDR", "redis:6379")
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Printf("redis disabled: %v", err)
	} else {
		redisLimiter, err := ratelimit.NewLimiter(redisClient, ratelimit.Config{
			Namespace: "orders",
			Limit:     100,
			Window:    time.Minute,
		})
		if err != nil {
			log.Printf("redis limiter disabled: %v", err)
		} else {
			limiter = redisLimiter
		}
	}

	var eventStore *postgres.Store
	postgresDSN := getenv("POSTGRES_DSN", "postgres://trading_engine:trading_engine@postgres:5432/trading_engine?sslmode=disable")
	pool, err := pgxpool.New(ctx, postgresDSN)
	if err != nil {
		log.Printf("postgres disabled: %v", err)
	} else if err := pool.Ping(ctx); err != nil {
		log.Printf("postgres disabled: %v", err)
		pool.Close()
	} else {
		store, err := postgres.NewStore(pool)
		if err != nil {
			log.Printf("postgres store disabled: %v", err)
			pool.Close()
		} else if err := store.ApplySchema(ctx); err != nil {
			log.Printf("postgres schema disabled: %v", err)
			pool.Close()
		} else {
			eventStore = store
		}
	}

	var publisher *kafka.Publisher
	kafkaBrokers := splitCSV(getenv("KAFKA_BROKERS", "kafka:9092"))
	kafkaTopic := getenv("KAFKA_TOPIC", "trading-engine-events")
	if len(kafkaBrokers) > 0 {
		kafkaWriter := kafka.NewWriter(kafkaBrokers, kafkaTopic)
		kafkaPublisher, err := kafka.NewPublisher(kafkaWriter)
		if err != nil {
			log.Printf("kafka publisher disabled: %v", err)
		} else {
			publisher = kafkaPublisher
		}
	}

	matcher := matching.NewEngine()
	server := api.NewServer(
		matcher,
		risk.NewEngine(risk.Config{MaxOrderQuantity: 1_000_000, MaxPosition: 1_000_000}),
		limiter,
		[]risk.Account{
			{
				ID:   "demo",
				Cash: 1_000_000_000,
				Positions: map[domain.Symbol]domain.Quantity{
					"BTC-USD": 500_000,
					"ETH-USD": 500_000,
				},
			},
		},
	)
	if eventStore != nil {
		server.UseEventStore(eventStore)
	}
	if publisher != nil {
		server.UseEventPublisher(publisher)
	}
	recovered := restoreEngine(ctx, server, eventStore)
	if !recovered {
		if err := server.SeedOrders(startupOrders()); err != nil {
			log.Printf("startup orders disabled: %v", err)
		} else if eventStore != nil {
			if err := eventStore.SaveSnapshot(ctx, matcher.Snapshot("BTC-USD")); err != nil {
				log.Printf("startup snapshot disabled: %v", err)
			}
		}
	} else {
		log.Printf("engine restored from postgres")
	}
	startMarketData(ctx, server)

	return http.ListenAndServe(addr, server.Router())
}

func restoreEngine(ctx context.Context, server *api.Server, store *postgres.Store) bool {
	if store == nil {
		return false
	}

	symbol := domain.Symbol(getenv("ENGINE_SNAPSHOT_SYMBOL", "BTC-USD"))
	snapshot, err := store.LatestSnapshot(ctx, symbol)
	if err != nil && !errors.Is(err, postgres.ErrSnapshotNotFound) {
		log.Printf("engine snapshot restore disabled: %v", err)
		return false
	}
	if errors.Is(err, postgres.ErrSnapshotNotFound) {
		snapshot = matching.Snapshot{}
	}

	events, err := store.EventsAfter(ctx, snapshot.Sequence)
	if err != nil {
		log.Printf("engine event replay disabled: %v", err)
		return false
	}
	trades, err := store.TradesBySymbol(ctx, symbol)
	if err != nil {
		log.Printf("engine trade restore disabled: %v", err)
		return false
	}
	if snapshot.Symbol == "" && len(events) == 0 && len(trades) == 0 {
		return false
	}
	if err := server.Restore(snapshot, events, trades); err != nil {
		log.Printf("engine restore disabled: %v", err)
		return false
	}
	log.Printf("engine restore loaded snapshot_sequence=%d events=%d trades=%d", snapshot.Sequence, len(events), len(trades))
	return true
}

func startMarketData(ctx context.Context, server *api.Server) {
	symbols := splitCSV(getenv("BINANCE_DOMAIN_SYMBOLS", "BTC-USDT,ETH-USDT"))
	for _, symbol := range symbols {
		startBinanceMarketData(ctx, server, domain.Symbol(symbol))
	}
}

func startBinanceMarketData(ctx context.Context, server *api.Server, symbol domain.Symbol) {
	book := marketdata.NewBook(symbol)
	server.UseMarketDataBook(symbol, book)
	streamSymbol := strings.ToLower(strings.ReplaceAll(string(symbol), "-", ""))

	feed, err := marketdata.NewBinanceDepthFeed(ctx, marketdata.BinanceDepthConfig{
		StreamSymbol:  streamSymbol,
		StreamName:    streamSymbol + "@depth20@100ms",
		DomainSymbol:  symbol,
		PriceScale:    100,
		QuantityScale: 100_000_000,
	})
	if err != nil {
		log.Printf("binance market data disabled for %s: %v", symbol, err)
		return
	}

	syncer := marketdata.NewSyncer(book, feed)
	go func() {
		if err := syncer.Run(context.Background()); err != nil {
			log.Printf("binance market data stopped for %s: %v", symbol, err)
		}
	}()
}

func startupOrders() []matching.Order {
	return []matching.Order{
		{
			ID:       "seed-buy-1",
			Symbol:   "BTC-USD",
			Side:     domain.SideBuy,
			Type:     domain.OrderTypeLimit,
			Price:    100,
			Quantity: 10,
		},
		{
			ID:       "seed-sell-1",
			Symbol:   "BTC-USD",
			Side:     domain.SideSell,
			Type:     domain.OrderTypeLimit,
			Price:    102,
			Quantity: 8,
		},
		{
			ID:       "seed-sell-2",
			Symbol:   "BTC-USD",
			Side:     domain.SideSell,
			Type:     domain.OrderTypeLimit,
			Price:    100,
			Quantity: 3,
		},
	}
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}
