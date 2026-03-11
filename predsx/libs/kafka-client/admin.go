package kafkaclient

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/predsx/predsx/libs/logger"
	"github.com/segmentio/kafka-go"
)

// EnsureTopics creates the required Kafka topics with the specified partition mapping
// if they do not already exist. It retries on failure to handle broker startup delays.
func EnsureTopics(ctx context.Context, brokers []string, topicPartitions map[string]int, log logger.Interface) error {
	if len(brokers) == 0 {
		return nil
	}
	
	broker := brokers[0]
	
	// Retry loop for establishing connection to Kafka broker
	var conn *kafka.Conn
	var err error
	for i := 0; i < 5; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		
		conn, err = kafka.Dial("tcp", broker)
		if err == nil {
			break
		}
		log.Warn("failed to connect to kafka admin, retrying...", "broker", broker, "error", err)
		time.Sleep(2 * time.Second)
	}
	
	if err != nil {
		return err
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		return err
	}
	
	var controllerConn *kafka.Conn
	controllerAddr := net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port))
	for i := 0; i < 5; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		controllerConn, err = kafka.Dial("tcp", controllerAddr)
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	if err != nil {
		return err
	}
	defer controllerConn.Close()

	var topicConfigs []kafka.TopicConfig
	for topic, partitions := range topicPartitions {
		topicConfigs = append(topicConfigs, kafka.TopicConfig{
			Topic:             topic,
			NumPartitions:     partitions,
			ReplicationFactor: 1,
		})
	}

	err = controllerConn.CreateTopics(topicConfigs...)
	if err != nil {
		return err
	}
	
	log.Info("kafka topics validated/created", "count", len(topicPartitions))
	return nil
}
