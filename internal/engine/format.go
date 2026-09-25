package engine

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Format applies a built-in formatter to a rendered value. A value the
// formatter can't parse is returned unchanged, so a surprise in the data is
// shown rather than hidden.
func Format(name, s string, now time.Time) string {
	switch name {
	case "ago":
		if t, ok := parseTime(s); ok {
			return ago(now.Sub(t))
		}
	case "duration":
		if d, ok := parseDuration(s); ok {
			return duration(d)
		}
	case "bytes":
		if n, err := strconv.ParseFloat(s, 64); err == nil {
			return byteSize(n)
		}
	case "basename":
		return s[strings.LastIndex(s, "/")+1:]
	case "bar":
		if b, ok := bar(s); ok {
			return b
		}
	}
	return s
}

// parseTime reads RFC 3339 (with or without zone; none = UTC) or Unix time in
// seconds (fractions allowed, as AWS prints them) or milliseconds.
func parseTime(s string) (time.Time, bool) {
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		if n > 1e12 {
			n /= 1000 // milliseconds
		}
		sec, frac := math.Modf(n)
		return time.Unix(int64(sec), int64(frac*1e9)), true
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func ago(d time.Duration) string {
	suffix, prefix := " ago", ""
	if d < 0 {
		d, suffix, prefix = -d, "", "in "
	}
	var n int64
	var unit string
	switch {
	case d < time.Minute:
		n, unit = int64(d/time.Second), "s"
	case d < time.Hour:
		n, unit = int64(d/time.Minute), "m"
	case d < 24*time.Hour:
		n, unit = int64(d/time.Hour), "h"
	default:
		n, unit = int64(d/(24*time.Hour)), "d"
	}
	return fmt.Sprintf("%s%d%s%s", prefix, n, unit, suffix)
}

// parseDuration reads seconds (a number) or a Go duration (90m, 1h30m).
func parseDuration(s string) (time.Duration, bool) {
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return time.Duration(n * float64(time.Second)), true
	}
	d, err := time.ParseDuration(s)
	return d, err == nil
}

func duration(d time.Duration) string {
	part := func(n int64, unit string, rest int64, restUnit string) string {
		if rest == 0 {
			return fmt.Sprintf("%d%s", n, unit)
		}
		return fmt.Sprintf("%d%s %d%s", n, unit, rest, restUnit)
	}
	switch {
	case d >= time.Hour:
		return part(int64(d/time.Hour), "h", int64(d%time.Hour/time.Minute), "m")
	case d >= time.Minute:
		return part(int64(d/time.Minute), "m", int64(d%time.Minute/time.Second), "s")
	}
	return fmt.Sprintf("%ds", int64(d/time.Second))
}

func byteSize(n float64) string {
	if n < 1024 {
		return fmt.Sprintf("%.0f B", n)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	i := -1
	for n >= 1024 && i < len(units)-1 {
		n /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %s", n, units[i])
}

// bar draws "n/m" as a 10-cell gauge followed by the numbers.
func bar(s string) (string, bool) {
	a, b, ok := strings.Cut(s, "/")
	n, err1 := strconv.ParseFloat(strings.TrimSpace(a), 64)
	m, err2 := strconv.ParseFloat(strings.TrimSpace(b), 64)
	if !ok || err1 != nil || err2 != nil || m <= 0 {
		return "", false
	}
	filled := int(math.Round(math.Max(0, math.Min(1, n/m)) * 10))
	return strings.Repeat("▰", filled) + strings.Repeat("▱", 10-filled) + " " + s, true
}
