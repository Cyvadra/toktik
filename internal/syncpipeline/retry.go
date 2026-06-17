package syncpipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"syscall"
	"time"
)

const (
	defaultRetryMaxAttempts  = 3
	defaultRetryInitialDelay = 5 * time.Second
	defaultRetryMaxDelay     = 30 * time.Second
)

type RetryOptions struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

func defaultRetryOptions(opts RetryOptions) RetryOptions {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = defaultRetryMaxAttempts
	}
	if opts.InitialDelay <= 0 {
		opts.InitialDelay = defaultRetryInitialDelay
	}
	if opts.MaxDelay <= 0 {
		opts.MaxDelay = defaultRetryMaxDelay
	}
	if opts.MaxDelay < opts.InitialDelay {
		opts.MaxDelay = opts.InitialDelay
	}
	return opts
}

func Retry(ctx context.Context, opts RetryOptions, logger *slog.Logger, operation string, fn func(context.Context) error) error {
	opts = defaultRetryOptions(opts)
	if logger == nil {
		logger = slog.Default()
	}
	var lastErr error
	delay := opts.InitialDelay
	for attempt := 1; attempt <= opts.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := fn(ctx)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt >= opts.MaxAttempts || !IsTransientDatabaseError(err) {
			return err
		}
		logger.Warn("transient database operation failed; retrying", "operation", operation, "attempt", attempt, "max_attempts", opts.MaxAttempts, "delay", delay, "err", err)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		delay *= 2
		if delay > opts.MaxDelay {
			delay = opts.MaxDelay
		}
	}
	return lastErr
}

func RetryValue[T any](ctx context.Context, opts RetryOptions, logger *slog.Logger, operation string, fn func(context.Context) (T, error)) (T, error) {
	var value T
	err := Retry(ctx, opts, logger, operation, func(ctx context.Context) error {
		var err error
		value, err = fn(ctx)
		return err
	})
	return value, err
}

func IsTransientDatabaseError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ETIMEDOUT) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	lower := strings.ToLower(fmt.Sprint(err))
	transientFragments := []string{
		"connection refused",
		"connection reset",
		"broken pipe",
		"i/o timeout",
		"timeout",
		"temporary failure",
		"server is not responding",
		"unexpected eof",
		"eof",
		"no such host",
		"cannot assign requested address",
	}
	for _, fragment := range transientFragments {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}
