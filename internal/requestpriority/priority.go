// Package requestpriority carries the request scheduling class through Go contexts.
package requestpriority

import (
	"context"
	"strings"
)

const Header = "X-Request-Priority"

type Priority string

const (
	Interactive Priority = "interactive"
	Default     Priority = "default"
	Background  Priority = "background"
)

type contextKey struct{}

// ParseHeader converts the public header contract into the internal priority.
// Missing and unrecognized values remain backward-compatible with default handling.
func ParseHeader(value string) Priority {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(Interactive):
		return Interactive
	case string(Background):
		return Background
	default:
		return Default
	}
}

func WithPriority(ctx context.Context, priority Priority) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, contextKey{}, normalize(priority))
}

func WithBackground(ctx context.Context) context.Context {
	return WithPriority(ctx, Background)
}

func FromContext(ctx context.Context) Priority {
	if ctx == nil {
		return Default
	}
	priority, ok := ctx.Value(contextKey{}).(Priority)
	if !ok {
		return Default
	}
	return normalize(priority)
}

func normalize(priority Priority) Priority {
	switch priority {
	case Interactive, Background:
		return priority
	default:
		return Default
	}
}