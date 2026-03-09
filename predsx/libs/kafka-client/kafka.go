package kafkaclient

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/predsx/predsx/libs/logger"
	"github.com/segmentio/kafka-go"
)

// ProducerInterface defines typed message publishing.
type ProducerInterface[T any] interface {
	Publish(ctx context.Context, key string, value T) error
	Close() error
}

// ConsumerInterface defines typed message fetching.
type ConsumerInterface[T any] interface {
	Fetch(ctx context.Context) (T, error)
	Close() error
}

// ClientInterface defines the Kafka client methods.
type ClientInterface interface {
	NewProducer(topic string) ProducerInterface[any] // Basic for any
	NewConsumer(topic string, groupID string) ConsumerInterface[any]
}

type Client struct {
	Brokers []string
	Logger  logger.Interface
}

func NewClient(brokers []string, log logger.Interface) *Client {
	return &Client{
		Brokers: brokers,
		Logger:  log,
	}
}

// TypedProducer implements ProducerInterface.
type TypedProducer[T any] struct {
	writer *kafka.Writer
	log    logger.Interface
}

func NewTypedProducer[T any](brokers []string, topic string, log logger.Interface) *TypedProducer[T] {
	return &TypedProducer[T]{
		writer: &kafka.Writer{
			Addr:     kafka.TCP(brokers...),
			Topic:    topic,
			Balancer: &kafka.LeastBytes{},
		},
		log: log,
	}
}

func (p *TypedProducer[T]) Publish(ctx context.Context, key string, value T) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	return p.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte(key),
		Value: data,
	})
}

func (p *TypedProducer[T]) Close() error {
	return p.writer.Close()
}

// TypedConsumer implements ConsumerInterface.
type TypedConsumer[T any] struct {
	reader *kafka.Reader
	log    logger.Interface
}

func NewTypedConsumer[T any](brokers []string, topic string, groupID string, log logger.Interface) *TypedConsumer[T] {
	return &TypedConsumer[T]{
		reader: kafka.NewReader(kafka.ReaderConfig{
			Brokers: brokers,
			Topic:   topic,
			GroupID: groupID,
		}),
		log: log,
	}
}

func (c *TypedConsumer[T]) Fetch(ctx context.Context) (T, error) {
	var result T
	msg, err := c.reader.ReadMessage(ctx)
	if err != nil {
		return result, err
	}

	if err := json.Unmarshal(msg.Value, &result); err != nil {
		return result, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	return result, nil
}

func (c *TypedConsumer[T]) Close() error {
	return c.reader.Close()
}
