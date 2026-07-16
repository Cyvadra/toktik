package diagnostics

import "fmt"

type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

type Diagnostic struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code,omitempty"`
	Message  string   `json:"message"`
	Function string   `json:"function,omitempty"`
	BarIndex *int     `json:"bar_index,omitempty"`
	Hint     string   `json:"hint,omitempty"`
}

func (d Diagnostic) String() string {
	if d.Code != "" {
		return fmt.Sprintf("%s: %s", d.Code, d.Message)
	}
	return d.Message
}

type Collector interface {
	Add(Diagnostic)
}

type List []Diagnostic

func (l *List) Add(d Diagnostic) {
	if l == nil || d.Message == "" {
		return
	}
	if d.Severity == "" {
		d.Severity = SeverityWarning
	}
	*l = append(*l, d)
}

func (l List) Strings() []string {
	if len(l) == 0 {
		return nil
	}
	out := make([]string, 0, len(l))
	for _, item := range l {
		out = append(out, item.String())
	}
	return out
}

// HasErrors reports whether the list contains any Error-severity diagnostic.
func (l List) HasErrors() bool {
	for _, item := range l {
		if item.Severity == SeverityError {
			return true
		}
	}
	return false
}
