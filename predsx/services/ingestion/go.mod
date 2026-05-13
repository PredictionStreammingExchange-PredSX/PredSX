module github.com/predsx/predsx/services/ingestion

go 1.24.0

require (
	github.com/ethereum/go-ethereum v1.17.2
	github.com/predsx/predsx/libs/clickhouse-client v0.0.0-00010101000000-000000000000
	github.com/predsx/predsx/libs/config v0.0.0
	github.com/predsx/predsx/libs/kafka-client v0.0.0-00010101000000-000000000000
	github.com/predsx/predsx/libs/logger v0.0.0
	github.com/predsx/predsx/libs/retry-utils v0.0.0-00010101000000-000000000000
	github.com/predsx/predsx/libs/schemas v0.0.0-00010101000000-000000000000
	github.com/predsx/predsx/libs/service v0.0.0
	github.com/predsx/predsx/libs/websocket-client v0.0.0-00010101000000-000000000000
)

require (
	github.com/ClickHouse/ch-go v0.61.5 // indirect
	github.com/ClickHouse/clickhouse-go/v2 v2.21.1 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/ProjectZKM/Ziren/crates/go-runtime/zkvm_runtime v0.0.0-20251001021608-1fe7b43fc4d6 // indirect
	github.com/andybalholm/brotli v1.1.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bits-and-blooms/bitset v1.20.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/consensys/gnark-crypto v0.18.1 // indirect
	github.com/crate-crypto/go-eth-kzg v1.5.0 // indirect
	github.com/deckarep/golang-set/v2 v2.6.0 // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.0.1 // indirect
	github.com/ethereum/c-kzg-4844/v2 v2.1.6 // indirect
	github.com/go-faster/city v1.0.1 // indirect
	github.com/go-faster/errors v0.7.1 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.1 // indirect
	github.com/holiman/uint256 v1.3.2 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/paulmach/orb v0.11.1 // indirect
	github.com/pierrec/lz4/v4 v4.1.21 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_golang v1.20.0 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.55.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/segmentio/asm v1.2.0 // indirect
	github.com/segmentio/kafka-go v0.4.47 // indirect
	github.com/shirou/gopsutil v3.21.11+incompatible // indirect
	github.com/shopspring/decimal v1.3.1 // indirect
	github.com/supranational/blst v0.3.16 // indirect
	github.com/tklauser/go-sysconf v0.3.12 // indirect
	github.com/tklauser/numcpus v0.6.1 // indirect
	github.com/yusufpapurcu/wmi v1.2.3 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.40.0 // indirect
	go.opentelemetry.io/otel/metric v1.40.0 // indirect
	go.opentelemetry.io/otel/trace v1.40.0 // indirect
	golang.org/x/net v0.47.0 // indirect
	golang.org/x/sync v0.18.0 // indirect
	golang.org/x/sys v0.40.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/predsx/predsx/libs/kafka-client => ../../libs/kafka-client

replace github.com/predsx/predsx/libs/postgres-client => ../../libs/postgres-client

replace github.com/predsx/predsx/libs/retry-utils => ../../libs/retry-utils

replace github.com/predsx/predsx/libs/redis-client => ../../libs/redis-client

replace github.com/predsx/predsx/libs/schemas => ../../libs/schemas

replace github.com/predsx/predsx/libs/service => ../../libs/service

replace github.com/predsx/predsx/libs/logger => ../../libs/logger

replace github.com/predsx/predsx/libs/config => ../../libs/config

replace github.com/predsx/predsx/libs/websocket-client => ../../libs/websocket-client

replace github.com/predsx/predsx/libs/crypto => ../../libs/crypto

replace github.com/predsx/predsx/libs/clickhouse-client => ../../libs/clickhouse-client
