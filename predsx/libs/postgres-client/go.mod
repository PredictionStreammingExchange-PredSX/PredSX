module github.com/predsx/predsx/libs/postgres-client

go 1.23

require (
	github.com/jackc/pgx/v5 v5.5.5
	github.com/predsx/predsx/libs/logger v0.0.0
)

replace github.com/predsx/predsx/libs/logger => ../logger
