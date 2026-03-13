package retry

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRetrySucceedsAfterFailures(t *testing.T) {
	t.Parallel()

	attempts := 0
	err := Retry(3, time.Millisecond, func() error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary failure")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Retry returned error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetryReturnsWrappedLastError(t *testing.T) {
	t.Parallel()

	attempts := 0
	err := Retry(3, time.Millisecond, func() error {
		attempts++
		return errors.New("still failing")
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
	if !strings.Contains(err.Error(), "retry failed after 3 attempts") {
		t.Fatalf("expected attempt count in error, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "still failing") {
		t.Fatalf("expected wrapped error in message, got %q", err.Error())
	}
}

func TestRetryRejectsInvalidAttempts(t *testing.T) {
	t.Parallel()

	err := Retry(0, 0, func() error { return nil })
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "attempts must be greater than zero" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRetryRejectsNilFunc(t *testing.T) {
	t.Parallel()

	err := Retry(1, 0, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "fn must not be nil" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRetryWaitsBetweenFailures(t *testing.T) {
	t.Parallel()

	delay := 20 * time.Millisecond
	start := time.Now()
	err := Retry(2, delay, func() error {
		return errors.New("fail")
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	elapsed := time.Since(start)
	if elapsed < delay {
		t.Fatalf("expected elapsed time >= %v, got %v", delay, elapsed)
	}
}
