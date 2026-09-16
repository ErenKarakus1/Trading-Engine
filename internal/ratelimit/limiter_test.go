package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestNewLimiterValidatesConfig(t *testing.T) {
	client := newFakeRedis()
	tests := []Config{
		{},
		{Namespace: "orders", Limit: 1},
		{Namespace: "orders", Window: time.Minute},
		{Limit: 1, Window: time.Minute},
	}

	for _, config := range tests {
		if _, err := NewLimiter(client, config); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("NewLimiter(%+v) error = %v, want %v", config, err, ErrInvalidConfig)
		}
	}
}

func TestNewLimiterRejectsNilClient(t *testing.T) {
	if _, err := NewLimiter(nil, Config{Namespace: "orders", Limit: 1, Window: time.Minute}); !errors.Is(err, ErrNilClient) {
		t.Fatalf("NewLimiter(nil) error = %v, want %v", err, ErrNilClient)
	}
}

func TestAllowPermitsRequestsWithinLimit(t *testing.T) {
	client := newFakeRedis()
	limiter := mustLimiter(t, client, Config{Namespace: "orders", Limit: 2, Window: time.Minute})

	if err := limiter.Allow(context.Background(), "account-1"); err != nil {
		t.Fatalf("Allow() first error = %v", err)
	}
	if err := limiter.Allow(context.Background(), "account-1"); err != nil {
		t.Fatalf("Allow() second error = %v", err)
	}

	if got := client.expirations["orders:account-1"]; got != time.Minute {
		t.Fatalf("expiration = %v, want %v", got, time.Minute)
	}
}

func TestAllowRejectsRequestsOverLimit(t *testing.T) {
	client := newFakeRedis()
	limiter := mustLimiter(t, client, Config{Namespace: "orders", Limit: 1, Window: time.Minute})

	if err := limiter.Allow(context.Background(), "account-1"); err != nil {
		t.Fatalf("Allow() first error = %v", err)
	}
	if err := limiter.Allow(context.Background(), "account-1"); !errors.Is(err, ErrLimited) {
		t.Fatalf("Allow() second error = %v, want %v", err, ErrLimited)
	}
}

func TestAllowKeepsSubjectsSeparate(t *testing.T) {
	client := newFakeRedis()
	limiter := mustLimiter(t, client, Config{Namespace: "orders", Limit: 1, Window: time.Minute})

	if err := limiter.Allow(context.Background(), "account-1"); err != nil {
		t.Fatalf("Allow(account-1) error = %v", err)
	}
	if err := limiter.Allow(context.Background(), "account-2"); err != nil {
		t.Fatalf("Allow(account-2) error = %v", err)
	}
}

func TestAllowPropagatesRedisErrors(t *testing.T) {
	want := errors.New("redis unavailable")
	client := newFakeRedis()
	client.incrErr = want
	limiter := mustLimiter(t, client, Config{Namespace: "orders", Limit: 1, Window: time.Minute})

	if err := limiter.Allow(context.Background(), "account-1"); !errors.Is(err, want) {
		t.Fatalf("Allow() error = %v, want %v", err, want)
	}
}

func mustLimiter(t *testing.T, client RedisClient, config Config) *Limiter {
	t.Helper()
	limiter, err := NewLimiter(client, config)
	if err != nil {
		t.Fatalf("NewLimiter() error = %v", err)
	}
	return limiter
}

type fakeRedis struct {
	counts      map[string]int64
	expirations map[string]time.Duration
	incrErr     error
	expireErr   error
}

func newFakeRedis() *fakeRedis {
	return &fakeRedis{
		counts:      make(map[string]int64),
		expirations: make(map[string]time.Duration),
	}
}

func (r *fakeRedis) Incr(_ context.Context, key string) *redis.IntCmd {
	cmd := redis.NewIntCmd(context.Background())
	if r.incrErr != nil {
		cmd.SetErr(r.incrErr)
		return cmd
	}
	r.counts[key]++
	cmd.SetVal(r.counts[key])
	return cmd
}

func (r *fakeRedis) Expire(_ context.Context, key string, expiration time.Duration) *redis.BoolCmd {
	cmd := redis.NewBoolCmd(context.Background())
	if r.expireErr != nil {
		cmd.SetErr(r.expireErr)
		return cmd
	}
	r.expirations[key] = expiration
	cmd.SetVal(true)
	return cmd
}
