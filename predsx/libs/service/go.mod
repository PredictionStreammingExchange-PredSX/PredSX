module github.com/predsx/predsx/libs/service

go 1.23

require (
	github.com/prometheus/client_golang v1.19.0
	github.com/predsx/predsx/libs/config v0.0.0
	github.com/predsx/predsx/libs/logger v0.0.0
)

replace github.com/predsx/predsx/libs/logger => ../logger
replace github.com/predsx/predsx/libs/config => ../config
