package retry

import (
	"context"
	"time"
)

// RetryFunc is the function to be retried.
type RetryFunc func() error

// Options contains retry configuration.
type Options struct {
	MaxAttempts int
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Multiplier      float64
}

// DefaultOptions provides a standard retry configuration.
func DefaultOptions() Options {
	return Options{
		MaxAttempts:     3,
		InitialInterval: 500 * time.Millisecond,
		MaxInterval:     5 * time.Second,
		Multiplier:      2.0,
	}
}

// Do executes the function with exponential backoff.
func Do(ctx context.Context, fn RetryFunc, opts Options) error {
	var err error
	interval := opts.InitialInterval

	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}

		if attempt == opts.MaxAttempts {
			break
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
			interval = time.Duration(float64(interval) * opts.Multiplier)
			if interval > opts.MaxInterval {
				interval = opts.MaxInterval
			}
		}
	}

	return err
}
