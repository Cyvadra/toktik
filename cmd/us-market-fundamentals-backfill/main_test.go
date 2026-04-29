package main
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Cyvadra/toktik/internal/config"
)

func TestConfirmTigerProviderUsageAcceptsExactPhrase(t *testing.T) {
	input := strings.NewReader(tigerConfirmationPhrase + "\n")
	var output bytes.Buffer

	if err := confirmTigerProviderUsage(input, &output); err != nil {
		t.Fatalf("expected confirmation to succeed, got %v", err)
	}
	if !strings.Contains(output.String(), tigerConfirmationPhrase) {
		t.Fatalf("expected prompt to mention confirmation phrase, got %q", output.String())
	}
}

func TestConfirmTigerProviderUsageRejectsMismatch(t *testing.T) {
	input := strings.NewReader("no\n")

	err := confirmTigerProviderUsage(input, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected confirmation mismatch to fail")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("expected mismatch error, got %v", err)
	}
}

func TestResolveBackfillProviderDisabledByDefault(t *testing.T) {
	_, err := resolveBackfillProvider(config.Runtime{}, "disabled", strings.NewReader(""), &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected disabled provider to fail")
	}
	if !strings.Contains(err.Error(), "sealed by default") {
		t.Fatalf("expected disabled-provider warning, got %v", err)
	}
}