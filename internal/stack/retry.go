// File: internal/stack/retry.go
// Brief: Retry classification and backoff.

package stack

import (
	"math/rand"
	"strings"
	"time"
)

func classifyError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "429") || strings.Contains(msg, "too many requests"):
		return "RATE_LIMIT"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "context deadline exceeded"):
		return "TIMEOUT"
	case strings.Contains(msg, "connection reset") || strings.Contains(msg, "broken pipe") || strings.Contains(msg, "eof"):
		return "TRANSPORT"
	case strings.Contains(msg, "temporarily unavailable"):
		return "UNAVAILABLE"
	case strings.Contains(msg, "internal error") || strings.Contains(msg, "server error") || strings.Contains(msg, " 5"):
		return "SERVER_5XX"
	default:
		return "OTHER"
	}
}

func isRetryableClass(class string) bool {
	switch class {
	case "RATE_LIMIT", "TIMEOUT", "TRANSPORT", "UNAVAILABLE", "SERVER_5XX":
		return true
	default:
		return false
	}
}

func retryBackoff(attempt int) time.Duration {
	// attempt is 1-based.
	base := 800 * time.Millisecond
	if attempt <= 1 {
		return jitter(base)
	}
	d := base * time.Duration(1<<uint(min(attempt-1, 6)))
	if d > 20*time.Second {
		d = 20 * time.Second
	}
	return jitter(d)
}

func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	// +/- 20%
	f := 0.8 + rand.Float64()*0.4
	return time.Duration(float64(d) * f)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
