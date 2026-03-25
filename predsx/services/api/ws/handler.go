package ws

import (
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/predsx/predsx/libs/logger"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// For this application, allow all CORS requests so the frontend dashboard can easily connect
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// ServeWS handles websocket requests from the peer.
func ServeWS(hub *Hub, w http.ResponseWriter, r *http.Request, log logger.Interface) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error("failed to upgrade connection to websocket", "error", err)
		return
	}

	client := NewClient(hub, conn, log)
	hub.register <- client

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()
}
