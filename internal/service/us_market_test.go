package service

import "testing"

func TestNormalizeUSSessionForAggregatedBars(t *testing.T) {
	tests := []struct {
		name     string
		session  string
		interval string
		want     string
		wantErr  bool
	}{
		{name: "1m defaults regular", session: "", interval: "1m", want: "regular"},
		{name: "1m extended allowed", session: "extended", interval: "1m", want: "extended"},
		{name: "1h defaults all", session: "", interval: "1h", want: "all"},
		{name: "1h regular coerces all", session: "regular", interval: "1h", want: "all"},
		{name: "1h all stays all", session: "all", interval: "1h", want: "all"},
		{name: "1h extended rejected", session: "extended", interval: "1h", wantErr: true},
		{name: "invalid session rejected", session: "overnight", interval: "1h", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeUSSession(tt.session, tt.interval)
			if (err != nil) != tt.wantErr {
				t.Fatalf("normalizeUSSession(%q, %q) error = %v, wantErr %v", tt.session, tt.interval, err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("normalizeUSSession(%q, %q) = %q, want %q", tt.session, tt.interval, got, tt.want)
			}
		})
	}
}
