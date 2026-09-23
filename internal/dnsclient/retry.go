package dnsclient

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

// finalError marks an error that no retry can change, such as an answer
// that the name does not exist. retryWithBackoff returns the wrapped error
// at once.
type finalError struct{ err error }

func (e *finalError) Error() string { return e.err.Error() }
func (e *finalError) Unwrap() error { return e.err }

// retryWithBackoff calls f up to maxAttempts times until it succeeds or
// returns a finalError. Between attempts it waits half the backoff plus a
// random share of it, and the backoff doubles up to maxBackoff. It returns
// how many times it called f.
func retryWithBackoff[T any](
	ctx context.Context,
	f func(attempt int) (T, error),
	maxAttempts int,
	initialBackoff time.Duration,
	maxBackoff time.Duration,
) (val T, attempts int, err error) {
	if maxAttempts < 1 {
		return val, 0, errors.New("maxAttempts must be positive")
	}

	backoff := min(initialBackoff, maxBackoff)

	for attempt := range maxAttempts {
		if cErr := ctx.Err(); cErr != nil {
			return val, attempts, cErr
		}

		attempts++
		val, err = f(attempt)
		if err == nil {
			return val, attempts, nil
		}
		if final := (*finalError)(nil); errors.As(err, &final) {
			return val, attempts, final.err
		}

		if attempt == maxAttempts-1 {
			break
		}

		//nolint:gosec // jitter timing here is non-security critical
		jitter := time.Duration(rand.N(int(backoff)))
		wait := backoff/2 + jitter

		select {
		case <-ctx.Done():
			return val, attempts, ctx.Err()
		case <-time.After(wait):
		}

		backoff = min(backoff*2, maxBackoff)
	}

	return val, attempts, err
}
