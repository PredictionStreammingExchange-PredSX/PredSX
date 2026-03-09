package main

import (
	"context"
	"fmt"
	"log"

	"github.com/predsx/predsx/libs/clickhouse-client"
	"github.com/predsx/predsx/libs/logger"
)

func main() {
	l := logger.NewLogger("info")
	ctx := context.Background()

	opts := clickhouse.Options{
		Addr: "localhost:9000",
		Database: "default",
		MaxConns: 10,
	}

	client, err := clickhouse.NewClient(opts, l)
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	if err := client.Ping(ctx); err != nil {
		fmt.Printf("ping failed: %v\n", err)
	}

	// Example insert
	err = client.Exec(ctx, "INSERT INTO prices (market_id, price) VALUES (?, ?)", "m1", 0.75)
	if err != nil {
		fmt.Printf("exec failed: %v\n", err)
	}
}
