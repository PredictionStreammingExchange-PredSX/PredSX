package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/predsx/predsx/libs/kafka-client"
	"github.com/predsx/predsx/libs/logger"
)

type MyEvent struct {
	ID        string    `json:"id"`
	Data      string    `json:"data"`
	Timestamp time.Time `json:"timestamp"`
}

func main() {
	l := logger.NewLogger("info")
	ctx := context.Background()
	brokers := []string{"localhost:9092"}

	// Create a typed producer
	producer := kafkaclient.NewTypedProducer[MyEvent](brokers, "test-topic", l)
	defer producer.Close()

	event := MyEvent{
		ID:        "1",
		Data:      "hello kafka",
		Timestamp: time.Now(),
	}

	if err := producer.Publish(ctx, event.ID, event); err != nil {
		log.Fatalf("failed to publish: %v", err)
	}
	fmt.Println("Published event")

	// Create a typed consumer
	consumer := kafkaclient.NewTypedConsumer[MyEvent](brokers, "test-topic", "test-group", l)
	defer consumer.Close()

	// In a real app, this would be in a loop
	ctxTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	received, err := consumer.Fetch(ctxTimeout)
	if err != nil {
		fmt.Printf("fetch failed: %v\n", err)
		return
	}

	fmt.Printf("Received: %+v\n", received)
}
