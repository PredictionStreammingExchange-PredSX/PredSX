package main

import (
	"context"
	"fmt"
	"log"

	"github.com/predsx/predsx/libs/logger"
	"github.com/predsx/predsx/libs/postgres-client"
)

func main() {
	l := logger.NewLogger("info")
	ctx := context.Background()

	connStr := "postgres://postgres:postgres@localhost:5432/predsx?sslmode=disable"
	client, err := postgres.NewClient(ctx, connStr, 10, l)
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	if err := client.Ping(ctx); err != nil {
		fmt.Printf("ping failed: %v\n", err)
	}

	// Example query
	var version string
	err = client.QueryRow(ctx, "SELECT version()").Scan(&version)
	if err != nil {
		fmt.Printf("query failed: %v\n", err)
	} else {
		fmt.Printf("Postgres version: %s\n", version)
	}
}
