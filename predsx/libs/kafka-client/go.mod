module github.com/predsx/predsx/libs/kafka-client

go 1.23

require (
	github.com/predsx/predsx/libs/logger v0.0.0
	github.com/segmentio/kafka-go v0.4.47
)

require (
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/pierrec/lz4/v4 v4.1.21 // indirect
	github.com/stretchr/testify v1.9.0 // indirect
	golang.org/x/net v0.26.0 // indirect
)

replace github.com/predsx/predsx/libs/logger => ../logger
