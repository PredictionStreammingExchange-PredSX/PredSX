module github.com/predsx/predsx/services/api

go 1.23

require (
	github.com/gorilla/mux v1.8.1
	github.com/predsx/predsx/libs/clickhouse-client v0.0.0
	github.com/predsx/predsx/libs/redis-client v0.0.0
	github.com/predsx/predsx/libs/service v0.0.0
	github.com/prometheus/client_golang v1.20.0
)

replace github.com/predsx/predsx/libs/clickhouse-client => ../../libs/clickhouse-client
replace github.com/predsx/predsx/libs/redis-client => ../../libs/redis-client
replace github.com/predsx/predsx/libs/service => ../../libs/service
replace github.com/predsx/predsx/libs/logger => ../../libs/logger
replace github.com/predsx/predsx/libs/config => ../../libs/config
replace github.com/predsx/predsx/libs/retry-utils => ../../libs/retry-utils
