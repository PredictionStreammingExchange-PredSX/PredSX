module github.com/predsx/predsx/libs/redis-client

go 1.23

require (
	github.com/redis/go-redis/v9 v9.5.1
	github.com/predsx/predsx/libs/logger v0.0.0
)

replace github.com/predsx/predsx/libs/logger => ../logger
