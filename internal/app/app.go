package app

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ErenKarakus1/Trading-Engine/internal/api"
	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
	"github.com/ErenKarakus1/Trading-Engine/internal/kafka"
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

	server := api.NewServer(
		matching.NewEngine(),
		risk.NewEngine(risk.Config{MaxOrderQuantity: 1_000_000, MaxPosition: 1_000_000}),
		limiter,
		[]risk.Account{
			{
				ID:   "demo",
				Cash: 1_000_000_000,
				Positions: map[domain.Symbol]domain.Quantity{
					"BTC-USD": 1_000_000,
					"ETH-USD": 1_000_000,
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

	return http.ListenAndServe(addr, server.Router())
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
