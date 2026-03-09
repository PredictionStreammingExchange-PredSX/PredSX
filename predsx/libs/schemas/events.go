package schemas

import (
	"encoding/json"
	"fmt"
	"time"
)

// RawWebsocketEvent captures raw data from upstream.
type RawWebsocketEvent struct {
	EventType string    `json:"event_type"`
	Token     string    `json:"token"`
	Payload   []byte    `json:"payload"`
	Timestamp time.Time `json:"timestamp"`
}

func (e *RawWebsocketEvent) Validate() error {
	if e.Token == "" || len(e.Payload) == 0 {
		return fmt.Errorf("token and payload are required")
	}
	return nil
}

// Version constants for schema evolution.
const (
	VersionV1 = "v1.0.0"
)

// MarketDiscovered is emitted when a new market is found.
type MarketDiscovered struct {
	ID          string    `json:"id" validate:"required"`
	Slug        string    `json:"slug" validate:"required"`
	Title       string    `json:"title"`
	Question    string    `json:"question"`
	ConditionID string    `json:"condition_id"`
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time"`
	Outcomes    []string  `json:"outcomes"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	Version     string    `json:"version"`
}

func (e *MarketDiscovered) Validate() error {
	if e.ID == "" || e.Slug == "" {
		return fmt.Errorf("id and slug are required")
	}
	return nil
}

// TokenExtracted is emitted after processing market tokens.
type TokenExtracted struct {
	MarketID string `json:"market_id" validate:"required"`
	TokenYes string `json:"token_yes" validate:"required"`
	TokenNo  string `json:"token_no" validate:"required"`
	Version  string `json:"version"`
}

func (e *TokenExtracted) Validate() error {
	if e.MarketID == "" || e.TokenYes == "" || e.TokenNo == "" {
		return fmt.Errorf("market_id, token_yes, and token_no are required")
	}
	return nil
}

// TradeEvent represents a single trade.
type TradeEvent struct {
	TradeID   string    `json:"trade_id" validate:"required"`
	Token     string    `json:"token" validate:"required"`
	MarketID  string    `json:"market_id" validate:"required"`
	Price     float64   `json:"price"`
	Size      float64   `json:"size"`
	Side      string    `json:"side"` // buy/sell
	Timestamp time.Time `json:"timestamp"`
	Version   string    `json:"version"`
}

func (e *TradeEvent) Validate() error {
	if e.TradeID == "" || e.MarketID == "" || e.Price <= 0 {
		return fmt.Errorf("invalid trade event data")
	}
	return nil
}

// OrderbookUpdate captures a snapshot or delta.
type OrderbookUpdate struct {
	Token     string      `json:"token" validate:"required"`
	MarketID  string      `json:"market_id" validate:"required"`
	Bids      []PriceLevel `json:"bids"`
	Asks      []PriceLevel `json:"asks"`
	BestBid   string      `json:"best_bid"`
	BestAsk   string      `json:"best_ask"`
	Timestamp time.Time   `json:"timestamp"`
	Version   string      `json:"version"`
}

type PriceLevel struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

func (e *OrderbookUpdate) Validate() error {
	if e.MarketID == "" {
		return fmt.Errorf("market_id is required")
	}
	return nil
}

// PriceUpdate is the calculated real-time price.
type PriceUpdate struct {
	MarketID       string    `json:"market_id" validate:"required"`
	Token          string    `json:"token"`
	Price          float64   `json:"price"` // mid_price
	MidPrice       float64   `json:"mid_price"`
	BestBid        float64   `json:"best_bid"`
	BestAsk        float64   `json:"best_ask"`
	Spread         float64   `json:"spread"`
	LastTradePrice float64   `json:"last_trade_price"`
	Volume24h      float64   `json:"volume_24h"`
	Timestamp      time.Time `json:"timestamp"`
	Version        string    `json:"version"`
}

func (e *PriceUpdate) Validate() error {
	if e.MarketID == "" || e.Price <= 0 {
		return fmt.Errorf("invalid price update data")
	}
	return nil
}

// NormalizedEvent is the final storage format.
type NormalizedEvent struct {
	EventType string            `json:"event_type" validate:"required"`
	MarketID  string            `json:"market_id" validate:"required"`
	Data      []byte            `json:"data"`
	Metadata  map[string]string `json:"metadata"`
	Timestamp time.Time         `json:"timestamp"`
	Version   string            `json:"version"`
}

func (e *NormalizedEvent) Validate() error {
	if e.EventType == "" || e.MarketID == "" {
		return fmt.Errorf("event_type and market_id are required")
	}
	return nil
}

// HistoricalEvent for backfilling.
type HistoricalEvent struct {
	MarketID  string    `json:"market_id" validate:"required"`
	Source    string    `json:"source"` // gamma, subgraph, rpc
	EventType string    `json:"event_type"`
	Data      []byte    `json:"data"`
	Timestamp time.Time `json:"timestamp"`
	Version   string    `json:"version"`
}

func (e *HistoricalEvent) Validate() error {
	if e.MarketID == "" {
		return fmt.Errorf("market_id is required")
	}
	return nil
}

// Serialization helpers
func MarshalEvent(v any) ([]byte, error) {
	return json.Marshal(v)
}

func UnmarshalEvent(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// Schema versioning helper
func GetVersion(data []byte) (string, error) {
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return "", err
	}
	if v, ok := m["version"].(string); ok {
		return v, nil
	}
	return "", fmt.Errorf("version not found")
}
