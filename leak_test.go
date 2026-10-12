package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/aerokube/ggr/config"
	"go.uber.org/goleak"
)

func setHosts(t *testing.T, m map[string]map[string]*config.Host) {
	t.Helper()
	lock.Lock()
	old := hosts
	hosts = m
	lock.Unlock()
	t.Cleanup(func() {
		lock.Lock()
		hosts = old
		lock.Unlock()
	})
}

func setDuration(t *testing.T, p *time.Duration, v time.Duration) {
	t.Helper()
	old := *p
	*p = v
	t.Cleanup(func() { *p = old })
}

func verifyNoLeaks(t *testing.T) {
	t.Helper()
	opt := goleak.IgnoreCurrent()
	t.Cleanup(func() {
		http.DefaultClient.CloseIdleConnections()
		goleak.VerifyNone(t, opt)
	})
}

func TestStatusNoGoroutineLeak(t *testing.T) {
	cases := []struct {
		name         string
		responseTime time.Duration
		clientWait   time.Duration
	}{
		{name: "ResponseTimeExceeded", responseTime: 50 * time.Millisecond},
		{name: "ClientDisconnected", responseTime: 10 * time.Second, clientWait: 50 * time.Millisecond},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			verifyNoLeaks(t)
			setDuration(t, &responseTime, c.responseTime)
			setDuration(t, &timeout, 10*time.Second)

			slow := NewSelenoid(silent)
			t.Cleanup(slow.Close)
			fast := NewSelenoid(empty)
			t.Cleanup(fast.Close)
			setHosts(t, map[string]map[string]*config.Host{
				"unknown": {slow.Sum: slow.Host, fast.Sum: fast.Host},
			})

			ggr := NewGGR()
			t.Cleanup(ggr.Close)
			ctx := context.Background()
			if c.clientWait > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, c.clientWait)
				defer cancel()
			}
			s, err := ggr.Status(ctx)
			if c.clientWait > 0 {
				if err == nil {
					t.Fatal("expected client timeout")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if s["total"] != 1.0 {
				t.Errorf("unexpected total: %v", s["total"])
			}
		})
	}
}
