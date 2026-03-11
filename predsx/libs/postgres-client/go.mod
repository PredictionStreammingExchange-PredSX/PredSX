module github.com/predsx/predsx/libs/postgres-client

go 1.23

require (
	github.com/jackc/pgx/v5 v5.5.5
	github.com/predsx/predsx/libs/logger v0.0.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jackc/puddle/v2 v2.2.1 // indirect
	github.com/stretchr/testify v1.9.0 // indirect
	golang.org/x/crypto v0.24.0 // indirect
	golang.org/x/sync v0.7.0 // indirect
	golang.org/x/text v0.16.0 // indirect
)

replace github.com/predsx/predsx/libs/logger => ../logger
