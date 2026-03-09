package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type PredSXClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewClient(baseURL string) *PredSXClient {
	return &PredSXClient{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *PredSXClient) GetMarkets() ([]map[string]interface{}, error) {
	resp, err := c.HTTPClient.Get(fmt.Sprintf("%s/v1/markets", c.BaseURL))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api error: %s", resp.Status)
	}

	var markets []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&markets); err != nil {
		return nil, err
	}
	return markets, nil
}

func (c *PredSXClient) GetPrice(marketID string) (map[string]interface{}, error) {
	resp, err := c.HTTPClient.Get(fmt.Sprintf("%s/v1/markets/%s/price", c.BaseURL, marketID))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api error: %s", resp.Status)
	}

	var price map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&price); err != nil {
		return nil, err
	}
	return price, nil
}

func (c *PredSXClient) GetOrderbook(marketID string) (map[string]interface{}, error) {
	resp, err := c.HTTPClient.Get(fmt.Sprintf("%s/v1/markets/%s/orderbook", c.BaseURL, marketID))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api error: %s", resp.Status)
	}

	var ob map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&ob); err != nil {
		return nil, err
	}
	return ob, nil
}

func (c *PredSXClient) GetTrades(marketID string) ([]map[string]interface{}, error) {
	resp, err := c.HTTPClient.Get(fmt.Sprintf("%s/v1/markets/%s/trades", c.BaseURL, marketID))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api error: %s", resp.Status)
	}

	var trades []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&trades); err != nil {
		return nil, err
	}
	return trades, nil
}
