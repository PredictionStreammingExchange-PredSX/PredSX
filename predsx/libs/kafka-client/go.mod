module github.com/predsx/predsx/libs/kafka-client

go 1.23

require (
	github.com/segmentio/kafka-go v0.4.47
	github.com/predsx/predsx/libs/logger v0.0.0
)

replace github.com/predsx/predsx/libs/logger => ../logger
