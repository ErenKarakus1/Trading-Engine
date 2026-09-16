package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

var (
	ErrInvalidConfig = errors.New("invalid rate limit config")
	ErrLimited       = errors.New("rate limit exceeded")
	ErrNilClient     = errors.New("nil redis client")
)

type RedisClient interface {
	Incr(context.Context, string) *redis.IntCmd
	Expire(context.Context, string, time.Duration) *redis.BoolCmd
}

type Config struct {
	Namespace string
	Limit     int64
	Window    time.Duration
}

type Limiter struct {
	client RedisClient
	config Config
}

func NewLimiter(client RedisClient, config Config) (*Limiter, error) {
	if client == nil {
		return nil, ErrNilClient
	}
	if config.Namespace == "" || config.Limit <= 0 || config.Window <= 0 {
		return nil, ErrInvalidConfig
	}
	return &Limiter{client: client, config: config}, nil
}

func (l *Limiter) Allow(ctx context.Context, subject string) error {
	if subject == "" {
		return ErrInvalidConfig
	}

	key := l.key(subject)
	count, err := l.client.Incr(ctx, key).Result()
	if err != nil {
		return err
	}
	if count == 1 {
		if err := l.client.Expire(ctx, key, l.config.Window).Err(); err != nil {
			return err
		}
	}
	if count > l.config.Limit {
		return ErrLimited
	}
	return nil
}

func (l *Limiter) AllowGin(c *gin.Context, subject string) error {
	return l.Allow(c.Request.Context(), subject)
}

func (l *Limiter) key(subject string) string {
	return fmt.Sprintf("%s:%s", l.config.Namespace, subject)
}
