package crypto

import (
	"crypto/sha1"
	"fmt"
)

// GenerateEventID computes a deterministic SHA1 hash for an event.
// The Level-10 Architecture defines the event_id as:
// event_id = SHA1(market_id + timestamp + price + size + side)
func GenerateEventID(marketID, timestamp, price, size, side string) string {
	payload := fmt.Sprintf("%s%s%s%s%s", marketID, timestamp, price, size, side)
	hash := sha1.Sum([]byte(payload))
	return fmt.Sprintf("%x", hash)
}
