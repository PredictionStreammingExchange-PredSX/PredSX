package ws

import (
	"context"

	"github.com/predsx/predsx/libs/logger"
)

// Hub maintains the set of active clients and broadcasts messages to the clients.
type Hub struct {
	// Registered clients.
	clients map[*Client]bool

	// Inbound messages from the server to broadcast to all clients.
	broadcast chan []byte

	// Register requests from the clients.
	register chan *Client

	// Unregister requests from clients.
	unregister chan *Client

	log logger.Interface
}

func NewHub(log logger.Interface) *Hub {
	return &Hub{
		broadcast:  make(chan []byte, 1024),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
		log:        log.With("component", "ws_hub"),
	}
}

func (h *Hub) Run(ctx context.Context) {
	h.log.Info("ws hub starting")
	for {
		select {
		case <-ctx.Done():
			h.log.Info("ws hub shutting down")
			return
		case client := <-h.register:
			h.clients[client] = true
			h.log.Info("client connected", "active_clients", len(h.clients))
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				h.log.Info("client disconnected", "active_clients", len(h.clients))
			}
		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
					h.log.Warn("dropped client due to slow buffer")
				}
			}
		}
	}
}

func (h *Hub) Broadcast(msg []byte) {
	h.broadcast <- msg
}
