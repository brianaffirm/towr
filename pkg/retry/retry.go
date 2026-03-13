package retry

import (
	"errors"
	"fmt"
	"time"
)

// Retry runs fn up to attempts times, waiting delay between failed attempts.
func Retry(attempts int, delay time.Duration, fn func() error) error {
	if attempts <= 0 {
		return errors.New("attempts must be greater than zero")
	}
	if fn == nil {
		return errors.New("fn must not be nil")
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := fn(); err != nil {
			lastErr = err
			if attempt < attempts {
				time.Sleep(delay)
			}
			continue
		}
		return nil
	}

	return fmt.Errorf("retry failed after %d attempts: %w", attempts, lastErr)
}
