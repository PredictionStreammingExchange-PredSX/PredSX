package schemas

import "time"

type MarketInfo struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Title     string    `json:"title"`
	Condition string    `json:"condition"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type TokenInfo struct {
	ID       string `json:"id"`
	MarketID string `json:"market_id"`
	Symbol   string `json:"symbol"`
	Decimals int    `json:"decimals"`
}
