package resilience

import (
	"context"
	"errors"
	"fmt"
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
	if policy.Multiplier <= 0 {
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
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return zero, ctx.Err()
		case <-timer.C:
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

// DoHTTPRequestWithRetry performs an HTTP request with the given retry policy.
// Retryable HTTP statuses (429/5xx) are retried until the attempt budget is exhausted.
func DoHTTPRequestWithRetry(
	ctx context.Context,
	client *http.Client,
	request *http.Request,
	policy RetryPolicy,
) (*http.Response, error) {
	effectiveAttempts := policy.Attempts
	if effectiveAttempts <= 0 {
		effectiveAttempts = 1
	}
	attemptCounter := 0
	return RetryWithResult(ctx, policy, IsRetryableHTTPRequestError, func(callCtx context.Context) (*http.Response, error) {
		attemptCounter++
		requestForAttempt := request.Clone(callCtx)
		response, err := client.Do(requestForAttempt) // #nosec G704 -- caller controls URL validation and request construction
		if err != nil {
			return nil, err
		}
		if IsRetryableHTTPStatus(response.StatusCode) {
			if attemptCounter >= effectiveAttempts {
				return response, nil
			}
			_ = response.Body.Close()
			return nil, retryableHTTPStatusError{statusCode: response.StatusCode}
		}
		return response, nil
	})
}

// IsRetryableHTTPRequestError classifies retryable transport/status errors for HTTP wrappers.
func IsRetryableHTTPRequestError(err error) bool {
	if IsRetryableNetworkError(err) {
		return true
	}
	var retryStatusError retryableHTTPStatusError
	return errors.As(err, &retryStatusError)
}

type retryableHTTPStatusError struct {
	statusCode int
}

func (err retryableHTTPStatusError) Error() string {
	return fmt.Sprintf("retryable HTTP status %d", err.statusCode)
}
