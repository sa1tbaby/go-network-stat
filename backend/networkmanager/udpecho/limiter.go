package udpecho

import (
	"time"
)

type rateWindow struct {
	start time.Time
	last  time.Time
	count int
}

type RateLimiter struct {
	windows map[string]rateWindow
	cleanup time.Time
}

// Allow caps amplification at 200 echoed datagrams per second per source IP,
// bounds retained peers to 1024, and reclaims peers idle for over one minute.
func (l *RateLimiter) Allow(ip string, now time.Time) bool {
	if l.windows == nil {
		l.windows = make(map[string]rateWindow)
	}

	if l.cleanup.IsZero() || now.Sub(l.cleanup) >= 10*time.Second {
		for key, window := range l.windows {
			if now.Sub(window.last) > time.Minute {
				delete(l.windows, key)
			}
		}

		l.cleanup = now
	}

	window, exists := l.windows[ip]
	if !exists {
		if len(l.windows) >= 1024 {
			return false
		}

		window = rateWindow{start: now}
	}

	if now.Sub(window.start) >= time.Second {
		window.start = now
		window.count = 0
	}

	window.last = now
	if window.count >= 200 {
		l.windows[ip] = window

		return false
	}

	window.count++
	l.windows[ip] = window

	return true
}
