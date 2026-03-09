package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/predsx/predsx/libs/logger"
)

// Interface defines the Postgres client methods.
type Interface interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Ping(ctx context.Context) error
	Close()
}

type Client struct {
	*pgxpool.Pool
	log logger.Interface
}

func NewClient(ctx context.Context, connStr string, maxConns int, log logger.Interface) (*Client, error) {
	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, err
	}
	
	config.MaxConns = int32(maxConns)

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}

	return &Client{
		Pool: pool,
		log:  log,
	}, nil
}

func (c *Client) Ping(ctx context.Context) error {
	return c.Pool.Ping(ctx)
}
