package schemas

import (
	"testing"
	"time"
)

func TestEventSerialization(t *testing.T) {
	event := &MarketDiscovered{
		ID:        "m1",
		Slug:      "test-market",
		Title:     "Test Market",
		CreatedAt: time.Now(),
		Version:   VersionV1,
	}

	data, err := MarshalEvent(event)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	version, err := GetVersion(data)
	if err != nil || version != VersionV1 {
		t.Errorf("expected version %s, got %s (err: %v)", VersionV1, version, err)
	}

	var decoded MarketDiscovered
	if err := UnmarshalEvent(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ID != event.ID {
		t.Errorf("expected ID %s, got %s", event.ID, decoded.ID)
	}
}

func TestEventValidation(t *testing.T) {
	t.Run("Valid TradeEvent", func(t *testing.T) {
		e := &TradeEvent{
			ID:       "t1",
			MarketID: "m1",
			Price:    10.5,
		}
		if err := e.Validate(); err != nil {
			t.Errorf("expected valid, got error: %v", err)
		}
	})

	t.Run("Invalid TradeEvent", func(t *testing.T) {
		e := &TradeEvent{
			ID: "t1",
		}
		if err := e.Validate(); err == nil {
			t.Error("expected error for missing market_id, got nil")
		}
	})
}
