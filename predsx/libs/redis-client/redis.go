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
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.BoolCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
	Scan(ctx context.Context, cursor uint64, match string, count int64) *redis.ScanCmd
	SAdd(ctx context.Context, key string, members ...interface{}) *redis.IntCmd
	SMembers(ctx context.Context, key string) *redis.StringSliceCmd
	SIsMember(ctx context.Context, key string, member interface{}) *redis.BoolCmd
	Exists(ctx context.Context, keys ...string) *redis.IntCmd
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

func (c *Client) SAdd(ctx context.Context, key string, members ...interface{}) *redis.IntCmd {
	return c.Client.SAdd(ctx, key, members...)
}

func (c *Client) SMembers(ctx context.Context, key string) *redis.StringSliceCmd {
	return c.Client.SMembers(ctx, key)
}

func (c *Client) SIsMember(ctx context.Context, key string, member interface{}) *redis.BoolCmd {
	return c.Client.SIsMember(ctx, key, member)
}
