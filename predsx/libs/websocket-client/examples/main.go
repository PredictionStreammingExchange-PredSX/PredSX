package main

import (
	"fmt"
	"log"

	"github.com/predsx/predsx/libs/logger"
	"github.com/predsx/predsx/libs/websocket-client"
)

func main() {
	l := logger.NewLogger("info")
	
	// Example client connecting to a public echo server
	url := "ws://echo.websocket.org"
	client, err := websocket.NewClient(url, l)
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer client.Close()

	msg := "hello websocket"
	if err := client.WriteMessage(1, []byte(msg)); err != nil {
		fmt.Printf("write failed: %v\n", err)
	}

	_, received, err := client.ReadMessage()
	if err != nil {
		fmt.Printf("read failed: %v\n", err)
	} else {
		fmt.Printf("Received: %s\n", string(received))
	}
}
