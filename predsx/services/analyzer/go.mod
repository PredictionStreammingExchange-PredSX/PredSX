module github.com/predsx/predsx/services/analyzer

go 1.23.0

require (
	github.com/predsx/predsx/libs/clickhouse-client v0.0.0
	github.com/predsx/predsx/libs/config v0.0.0
	github.com/predsx/predsx/libs/crypto v0.0.0
	github.com/predsx/predsx/libs/kafka-client v0.0.0
	github.com/predsx/predsx/libs/redis-client v0.0.0
	github.com/predsx/predsx/libs/schemas v0.0.0
	github.com/predsx/predsx/libs/service v0.0.0
)

require (
	github.com/ClickHouse/ch-go v0.61.5 // indirect
	github.com/ClickHouse/clickhouse-go/v2 v2.21.1 // indirect
	github.com/andybalholm/brotli v1.1.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/go-faster/city v1.0.1 // indirect
	github.com/go-faster/errors v0.7.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/paulmach/orb v0.11.1 // indirect
	github.com/pierrec/lz4/v4 v4.1.21 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/predsx/predsx/libs/logger v0.0.0 // indirect
	github.com/prometheus/client_golang v1.20.0 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.55.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/redis/go-redis/v9 v9.5.1 // indirect
	github.com/segmentio/asm v1.2.0 // indirect
	github.com/segmentio/kafka-go v0.4.47 // indirect
	github.com/shopspring/decimal v1.3.1 // indirect
	go.opentelemetry.io/otel v1.24.0 // indirect
	go.opentelemetry.io/otel/trace v1.24.0 // indirect
	golang.org/x/sys v0.22.0 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/predsx/predsx/libs/clickhouse-client => ../../libs/clickhouse-client

replace github.com/predsx/predsx/libs/config => ../../libs/config

replace github.com/predsx/predsx/libs/crypto => ../../libs/crypto

replace github.com/predsx/predsx/libs/kafka-client => ../../libs/kafka-client

replace github.com/predsx/predsx/libs/redis-client => ../../libs/redis-client

replace github.com/predsx/predsx/libs/schemas => ../../libs/schemas

replace github.com/predsx/predsx/libs/service => ../../libs/service

replace github.com/predsx/predsx/libs/logger => ../../libs/logger
