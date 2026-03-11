module github.com/predsx/predsx/libs/websocket-client

go 1.23

require (
	github.com/gorilla/websocket v1.5.1
	github.com/predsx/predsx/libs/logger v0.0.0
)

require golang.org/x/net v0.26.0 // indirect

replace github.com/predsx/predsx/libs/logger => ../logger
