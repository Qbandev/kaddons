package resilience

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryWithResult_SucceedsAfterRetries(t *testing.T) {
	policy := RetryPolicy{
		Attempts:     3,
		InitialDelay: 0,
		MaxDelay:     0,
		Multiplier:   2,
	}
	attempts := 0
	value, err := RetryWithResult(context.Background(), policy, func(err error) bool {
		return errors.Is(err, errRetryable)
	}, func(ctx context.Context) (string, error) {
		attempts++
		if attempts < 3 {
			return "", errRetryable
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("RetryWithResult() unexpected error: %v", err)
	}
	if value != "ok" {
		t.Fatalf("RetryWithResult() value = %q, want %q", value, "ok")
	}
	if attempts != 3 {
		t.Fatalf("RetryWithResult() attempts = %d, want 3", attempts)
	}
}

func TestRetryWithResult_StopsOnNonRetryableError(t *testing.T) {
	policy := RetryPolicy{
		Attempts:     5,
		InitialDelay: 0,
		MaxDelay:     0,
		Multiplier:   2,
	}
	attempts := 0
	_, err := RetryWithResult(context.Background(), policy, func(err error) bool {
		return errors.Is(err, errRetryable)
	}, func(ctx context.Context) (string, error) {
		attempts++
		return "", errTerminal
	})
	if !errors.Is(err, errTerminal) {
		t.Fatalf("RetryWithResult() error = %v, want %v", err, errTerminal)
	}
	if attempts != 1 {
		t.Fatalf("RetryWithResult() attempts = %d, want 1", attempts)
	}
}

func TestRetryPolicy_BackoffCapped(t *testing.T) {
	policy := RetryPolicy{
		Attempts:     4,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     250 * time.Millisecond,
		Multiplier:   2,
	}

	if got := policy.backoff(1); got != 100*time.Millisecond {
		t.Fatalf("backoff(1) = %v, want 100ms", got)
	}
	if got := policy.backoff(2); got != 200*time.Millisecond {
		t.Fatalf("backoff(2) = %v, want 200ms", got)
	}
	if got := policy.backoff(3); got != 250*time.Millisecond {
		t.Fatalf("backoff(3) = %v, want 250ms", got)
	}
}

func TestRetryWithResult_ReturnsContextErrorWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	policy := RetryPolicy{
		Attempts:     3,
		InitialDelay: 200 * time.Millisecond,
		MaxDelay:     200 * time.Millisecond,
		Multiplier:   2,
	}

	attempts := 0
	_, err := RetryWithResult(ctx, policy, func(err error) bool {
		return errors.Is(err, errRetryable)
	}, func(callCtx context.Context) (string, error) {
		attempts++
		cancel() // cancel before retry backoff wait begins
		return "", errRetryable
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RetryWithResult() error = %v, want context.Canceled", err)
	}
	if attempts != 1 {
		t.Fatalf("RetryWithResult() attempts = %d, want 1", attempts)
	}
}

var (
	errRetryable = errors.New("retryable")
	errTerminal  = errors.New("terminal")
)
