package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDo(t *testing.T) {
	ctx := context.Background()

	t.Run("Success on first attempt", func(t *testing.T) {
		attempts := 0
		err := Do(ctx, func() error {
			attempts++
			return nil
		}, DefaultOptions())

		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if attempts != 1 {
			t.Errorf("expected 1 attempt, got %d", attempts)
		}
	})

	t.Run("Success after retries", func(t *testing.T) {
		attempts := 0
		err := Do(ctx, func() error {
			attempts++
			if attempts < 3 {
				return errors.New("fail")
			}
			return nil
		}, Options{
			MaxAttempts:     5,
			InitialInterval: 1 * time.Millisecond,
			MaxInterval:     10 * time.Millisecond,
			Multiplier:      2.0,
		})

		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if attempts != 3 {
			t.Errorf("expected 3 attempts, got %d", attempts)
		}
	})

	t.Run("Failure after max attempts", func(t *testing.T) {
		attempts := 0
		expectedErr := errors.New("permanent failure")
		err := Do(ctx, func() error {
			attempts++
			return expectedErr
		}, Options{
			MaxAttempts:     3,
			InitialInterval: 1 * time.Millisecond,
			MaxInterval:     10 * time.Millisecond,
			Multiplier:      2.0,
		})

		if !errors.Is(err, expectedErr) {
			t.Errorf("expected error %v, got %v", expectedErr, err)
		}
		if attempts != 3 {
			t.Errorf("expected 3 attempts, got %d", attempts)
		}
	})
}
