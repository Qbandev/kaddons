package resilience

import (
	"context"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"strings"
	"time"
)

// RetryPolicy defines deterministic retry behavior (no jitter).
type RetryPolicy struct {
	Attempts     int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	Multiplier   float64
}

// Retry executes fn with deterministic backoff until success, context cancellation,
// or retry budget exhaustion.
func Retry(ctx context.Context, policy RetryPolicy, isRetryable func(error) bool, fn func(context.Context) error) error {
	_, err := RetryWithResult(ctx, policy, isRetryable, func(callCtx context.Context) (struct{}, error) {
		return struct{}{}, fn(callCtx)
	})
	return err
}

// RetryWithResult executes fn with deterministic backoff and returns fn result.
func RetryWithResult[T any](
	ctx context.Context,
	policy RetryPolicy,
	isRetryable func(error) bool,
	fn func(context.Context) (T, error),
) (T, error) {
	var zero T
	attempts := policy.Attempts
	if attempts <= 0 {
		attempts = 1
	}
	if policy.Multiplier <= 1 {
		policy.Multiplier = 2
	}
	if policy.InitialDelay < 0 {
		policy.InitialDelay = 0
	}
	if policy.MaxDelay < 0 {
		policy.MaxDelay = 0
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		result, err := fn(ctx)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if attempt == attempts || !isRetryable(err) {
			return zero, err
		}

		delay := policy.backoff(attempt)
		if delay <= 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(delay):
		}
	}
	return zero, lastErr
}

func (policy RetryPolicy) backoff(attempt int) time.Duration {
	if attempt <= 0 || policy.InitialDelay <= 0 {
		return 0
	}
	scale := math.Pow(policy.Multiplier, float64(attempt-1))
	delay := time.Duration(float64(policy.InitialDelay) * scale)
	if policy.MaxDelay > 0 && delay > policy.MaxDelay {
		return policy.MaxDelay
	}
	return delay
}

// IsRetryableHTTPStatus reports status codes that are commonly safe to retry.
func IsRetryableHTTPStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError
}

// IsRetryableNetworkError reports transport-level transient failures.
func IsRetryableNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) {
		return true
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return true
	}
	errText := strings.ToLower(err.Error())
	return strings.Contains(errText, "timeout") ||
		strings.Contains(errText, "temporarily unavailable") ||
		strings.Contains(errText, "connection refused") ||
		strings.Contains(errText, "connection reset by peer") ||
		strings.Contains(errText, "i/o timeout") ||
		strings.Contains(errText, "unexpected eof") ||
		strings.Contains(errText, "tls handshake timeout") ||
		strings.Contains(errText, "service unavailable")
}
