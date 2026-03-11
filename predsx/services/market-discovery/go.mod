module github.com/predsx/predsx/services/market-discovery

go 1.23

require (
	github.com/predsx/predsx/libs/config v0.0.0
	github.com/predsx/predsx/libs/kafka-client v0.0.0
	github.com/predsx/predsx/libs/retry-utils v0.0.0
	github.com/predsx/predsx/libs/schemas v0.0.0
	github.com/predsx/predsx/libs/service v0.0.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pierrec/lz4/v4 v4.1.21 // indirect
	github.com/predsx/predsx/libs/logger v0.0.0 // indirect
	github.com/prometheus/client_golang v1.20.0 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.55.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/segmentio/kafka-go v0.4.47 // indirect
	golang.org/x/sys v0.22.0 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
)

replace github.com/predsx/predsx/libs/kafka-client => ../../libs/kafka-client

replace github.com/predsx/predsx/libs/postgres-client => ../../libs/postgres-client

replace github.com/predsx/predsx/libs/retry-utils => ../../libs/retry-utils

replace github.com/predsx/predsx/libs/schemas => ../../libs/schemas

replace github.com/predsx/predsx/libs/service => ../../libs/service

replace github.com/predsx/predsx/libs/logger => ../../libs/logger

replace github.com/predsx/predsx/libs/config => ../../libs/config
