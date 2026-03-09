module github.com/predsx/predsx/services/backfill

go 1.23

require (
	github.com/predsx/predsx/libs/kafka-client v0.0.0
	github.com/predsx/predsx/libs/postgres-client v0.0.0
	github.com/predsx/predsx/libs/schemas v0.0.0
	github.com/predsx/predsx/libs/service v0.0.0
)

replace github.com/predsx/predsx/libs/kafka-client => ../../libs/kafka-client
replace github.com/predsx/predsx/libs/postgres-client => ../../libs/postgres-client
replace github.com/predsx/predsx/libs/schemas => ../../libs/schemas
replace github.com/predsx/predsx/libs/service => ../../libs/service
replace github.com/predsx/predsx/libs/logger => ../../libs/logger
replace github.com/predsx/predsx/libs/config => ../../libs/config
replace github.com/predsx/predsx/libs/retry-utils => ../../libs/retry-utils
