package requestpriority

import (
	"context"
	"testing"
)

func TestParseHeader(t *testing.T) {
	tests := map[string]Priority{
		"interactive":  Interactive,
		" BACKGROUND ": Background,
		"INTERACTIVE":  Interactive,
		"":             Default,
		"unknown":      Default,
	}
	for input, want := range tests {
		if got := ParseHeader(input); got != want {
			t.Errorf("ParseHeader(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestContextPriority(t *testing.T) {
	if got := FromContext(context.Background()); got != Default {
		t.Fatalf("missing context priority = %q, want %q", got, Default)
	}
	if got := FromContext(WithBackground(context.Background())); got != Background {
		t.Fatalf("background context priority = %q, want %q", got, Background)
	}
}