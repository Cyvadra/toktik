package diagnostics

import "testing"

func TestListHasErrors(t *testing.T) {
	var warningsOnly List
	warningsOnly.Add(Diagnostic{Severity: SeverityWarning, Message: "warn"})
	if warningsOnly.HasErrors() {
		t.Fatalf("warnings-only list should not report HasErrors")
	}

	var withError List
	withError.Add(Diagnostic{Severity: SeverityWarning, Message: "warn"})
	withError.Add(Diagnostic{Severity: SeverityError, Message: "boom"})
	if !withError.HasErrors() {
		t.Fatalf("list containing an error diagnostic should report HasErrors")
	}
}
