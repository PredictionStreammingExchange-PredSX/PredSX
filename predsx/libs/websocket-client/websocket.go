package websocket

import (
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/predsx/predsx/libs/logger"
)

// Interface defines the WebSocket client methods.
type Interface interface {
	ReadMessage() (messageType int, p []byte, err error)
	WriteMessage(messageType int, data []byte) error
	Close() error
}

type Client struct {
	Conn *websocket.Conn
	log  logger.Interface
}

func NewClient(url string, log logger.Interface) (Interface, error) {
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, err
	}

	return &Client{
		Conn: conn,
		log:  log,
	}, nil
}

func (c *Client) ReadMessage() (int, []byte, error) {
	return c.Conn.ReadMessage()
}

func (c *Client) WriteMessage(messageType int, data []byte) error {
	return c.Conn.WriteMessage(messageType, data)
}

func (c *Client) Close() error {
	return c.Conn.Close()
}

// ServerInterface defines the WebSocket server methods.
type ServerInterface interface {
	Upgrade(w http.ResponseWriter, r *http.Request) (Interface, error)
}

type Server struct {
	Upgrader websocket.Upgrader
	log      logger.Interface
}

func NewServer(log logger.Interface) *Server {
	return &Server{
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		log: log,
	}
}

func (s *Server) Upgrade(w http.ResponseWriter, r *http.Request) (Interface, error) {
	conn, err := s.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		return nil, err
	}
	return &Client{Conn: conn, log: s.log}, nil
}
