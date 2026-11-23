package core

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// Pinger is the dead-man's switch heartbeat (design 5.8): at the end of every
// 5-minute tick the bot GETs HEALTHCHECK_URL, proving the check loop actually
// ran — not just that the process exists. healthchecks.io (period 5 min,
// grace 10 min) alerts Khang after ~15 min of silence.
type Pinger struct {
	url    string
	log    *slog.Logger
	client *http.Client
}

// pingTimeout bounds one ping attempt.
const pingTimeout = 10 * time.Second

func NewPinger(url string, log *slog.Logger) *Pinger {
	return &Pinger{
		url:    url,
		log:    log,
		client: &http.Client{Timeout: pingTimeout},
	}
}

// Ping fires one GET in its own goroutine: a failed ping is logged and not
// retried (the next tick pings again) and never blocks or delays the checks.
func (p *Pinger) Ping() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, nil)
		if err != nil {
			p.log.Error("healthcheck ping failed", "err", err)
			return
		}
		resp, err := p.client.Do(req)
		if err != nil {
			p.log.Error("healthcheck ping failed (not retried; next tick pings again)", "err", err)
			return
		}
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			p.log.Error("healthcheck ping failed (not retried; next tick pings again)", "status", resp.StatusCode)
		}
	}()
}
