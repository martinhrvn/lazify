package engine

import (
	"testing"
	"time"
)

func TestFormatters(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	tests := []struct{ format, in, want string }{
		{"ago", "2026-09-25T11:59:48Z", "12s ago"},
		{"ago", "2026-09-25T11:57:00Z", "3m ago"},
		{"ago", "2026-09-25T07:00:00+00:00", "5h ago"},
		{"ago", "2026-09-23T12:00:00Z", "2d ago"},
		{"ago", "2026-09-25T12:03:00Z", "in 3m"},
		{"ago", "1790337300", "5m ago"},          // unix seconds (11:55)
		{"ago", "1790337300000", "5m ago"},       // unix milliseconds
		{"ago", "1790337299.5", "5m ago"},        // fractional seconds (AWS)
		{"ago", "2026-09-25 11:59:00", "1m ago"}, // no zone: UTC
		{"ago", "yesterday", "yesterday"},        // unparseable: raw
		{"duration", "3725", "1h 2m"},
		{"duration", "45", "45s"},
		{"duration", "90m", "1h 30m"},
		{"duration", "2d", "2d"},
		{"bytes", "1258291", "1.2 MiB"},
		{"bytes", "512", "512 B"},
		{"bytes", "5368709120", "5.0 GiB"},
		{"bytes", "lots", "lots"},
		{"basename", "arn:aws:ecs:eu-west-1:1:task/web/0af3c1", "0af3c1"},
		{"basename", "plain", "plain"},
		{"bar", "3/5", "▰▰▰▰▰▰▱▱▱▱ 3/5"},
		{"bar", "0/2", "▱▱▱▱▱▱▱▱▱▱ 0/2"},
		{"bar", "4/2", "▰▰▰▰▰▰▰▰▰▰ 4/2"}, // over: full
		{"bar", "1/0", "1/0"},
		{"", "unchanged", "unchanged"},
	}
	for _, tt := range tests {
		if got := Format(tt.format, tt.in, now); got != tt.want {
			t.Errorf("Format(%q, %q) = %q, want %q", tt.format, tt.in, got, tt.want)
		}
	}
}
