package main

import (
	"context"
	"fmt"
	"time"

	"github.com/predsx/predsx/libs/logger"
	"github.com/predsx/predsx/libs/redis-client"
)

func main() {
	l := logger.NewLogger("info")
	ctx := context.Background()

	opts := redisclient.Options{
		Addr:     "localhost:6379",
		PoolSize: 10,
	}

	client := redisclient.NewClient(opts, l)
	defer client.Close()

	if err := client.Ping(ctx); err != nil {
		fmt.Printf("ping failed: %v\n", err)
	}

	key := "test-key"
	val := "test-value"

	client.Set(ctx, key, val, 1*time.Hour)
	fmt.Println("Set value")

	res, err := client.Get(ctx, key).Result()
	if err != nil {
		fmt.Printf("get failed: %v\n", err)
	} else {
		fmt.Printf("Get value: %s\n", res)
	}
}
