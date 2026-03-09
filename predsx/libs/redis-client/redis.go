package redisclient

import (
	"context"
	"time"

	"github.com/predsx/predsx/libs/logger"
	"github.com/redis/go-redis/v9"
)

// Interface defines the Redis client methods.
type Interface interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
	Ping(ctx context.Context) error
	Close() error
}

type Client struct {
	*redis.Client
	log logger.Interface
}

type Options struct {
	Addr     string
	Password string
	DB       int
	PoolSize int
}

func NewClient(opts Options, log logger.Interface) *Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     opts.Addr,
		Password: opts.Password,
		DB:       opts.DB,
		PoolSize: opts.PoolSize,
	})

	return &Client{
		Client: rdb,
		log:    log,
	}
}

func (c *Client) Ping(ctx context.Context) error {
	return c.Client.Ping(ctx).Err()
}
