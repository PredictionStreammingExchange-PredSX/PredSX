module github.com/predsx/predsx/libs/clickhouse-client

go 1.23

require (
	github.com/ClickHouse/clickhouse-go/v2 v2.21.1
	github.com/predsx/predsx/libs/logger v0.0.0
)

replace github.com/predsx/predsx/libs/logger => ../logger
