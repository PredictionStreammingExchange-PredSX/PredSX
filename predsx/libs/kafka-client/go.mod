module github.com/predsx/predsx/libs/kafka-client

go 1.24.0

require (
	github.com/predsx/predsx/libs/logger v0.0.0
	github.com/segmentio/kafka-go v0.4.47
)

require (
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/pierrec/lz4/v4 v4.1.21 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	golang.org/x/net v0.47.0 // indirect
)

replace github.com/predsx/predsx/libs/logger => ../logger
