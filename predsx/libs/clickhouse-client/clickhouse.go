package clickhouse

import (
	"context"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/predsx/predsx/libs/logger"
)

// Interface defines the ClickHouse client methods.
type Interface interface {
	Exec(ctx context.Context, query string, args ...any) error
	Query(ctx context.Context, query string, args ...any) (any, error)
	Ping(ctx context.Context) error
	Close() error
}

type Client struct {
	clickhouse.Conn
	log logger.Interface
}

type Options struct {
	Addr     string
	Database string
	User     string
	Password string
	MaxConns int
}

func NewClient(opts Options, log logger.Interface) (*Client, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{opts.Addr},
		Auth: clickhouse.Auth{
			Database: opts.Database,
			Username: opts.User,
			Password: opts.Password,
		},
		MaxOpenConns: opts.MaxConns,
		MaxIdleConns: opts.MaxConns / 2,
		ConnMaxLifetime: 10 * time.Minute,
	})
	if err != nil {
		return nil, err
	}

	return &Client{
		Conn: conn,
		log:  log,
	}, nil
}

func (c *Client) Ping(ctx context.Context) error {
	return c.Conn.Ping(ctx)
}

func (c *Client) Query(ctx context.Context, query string, args ...any) (any, error) {
	return c.Conn.Query(ctx, query, args...)
}

func (c *Client) Close() error {
	return c.Conn.Close()
}
