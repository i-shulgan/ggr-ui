package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aerokube/ggr/config"
)

func setBool(t *testing.T, p *bool, v bool) {
	t.Helper()
	old := *p
	*p = v
	t.Cleanup(func() { *p = old })
}

func getStatus(t *testing.T, ggr *GGR, user string) (int, Status) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ggr.URL+paths.Status, nil)
	if err != nil {
		t.Fatal(err)
	}
	if user != "" {
		req.SetBasicAuth(user, "password")
	}
	req.Header.Set("X-Forwarded-For", "10.0.0.1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("unexpected content type: %s", ct)
	}
	buf, err := ReadResponseBody(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	s, err := DecodeResponse(buf)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, s
}

// countingSelenoid serves a /status with total=n and counts calls.
func countingSelenoid(t *testing.T, n int, calls *atomic.Int32, auth bool) *Selenoid {
	t.Helper()
	s := NewSelenoid(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if auth {
			if u, p, ok := r.BasicAuth(); !ok || u != "selenoid" || p != "secret" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		_, _ = fmt.Fprintf(w, `{"total":%d,"used":0,"queued":0,"pending":0,"browsers":{}}`, n)
	}))
	t.Cleanup(s.Close)
	if auth {
		s.Host.Username, s.Host.Password = "selenoid", "secret"
	}
	return s
}

func TestStatusAccessModes(t *testing.T) {
	var aliceCalls, bobCalls atomic.Int32
	alice := countingSelenoid(t, 1, &aliceCalls, true)
	bob := countingSelenoid(t, 2, &bobCalls, false)
	setHosts(t, map[string]map[string]*config.Host{
		"alice": {alice.Sum: alice.Host},
		"bob":   {bob.Sum: bob.Host},
	})
	ggr := NewGGR()
	defer ggr.Close()

	t.Run("AllHostsByDefault", func(t *testing.T) {
		code, s := getStatus(t, ggr, "")
		if code != http.StatusOK || s["total"] != 3.0 {
			t.Errorf("unexpected response: %d %v", code, s)
		}
	})
	t.Run("AuthenticatedOnly", func(t *testing.T) {
		setBool(t, &authenticatedAccessOnly, true)
		aliceCalls.Store(0)
		bobCalls.Store(0)
		code, s := getStatus(t, ggr, "alice")
		if code != http.StatusOK || s["total"] != 1.0 {
			t.Errorf("unexpected response: %d %v", code, s)
		}
		if aliceCalls.Load() != 1 || bobCalls.Load() != 0 {
			t.Errorf("unexpected calls: alice=%d bob=%d", aliceCalls.Load(), bobCalls.Load())
		}
	})
	t.Run("AuthenticatedOnlyUnknownUser", func(t *testing.T) {
		setBool(t, &authenticatedAccessOnly, true)
		if code, _ := getStatus(t, ggr, ""); code != http.StatusBadRequest {
			t.Errorf("unexpected code: %d", code)
		}
		if code, _ := getStatus(t, ggr, "mallory"); code != http.StatusBadRequest {
			t.Errorf("unexpected code: %d", code)
		}
	})
	t.Run("Guest", func(t *testing.T) {
		setBool(t, &authenticatedAccessOnly, true)
		setBool(t, &guestAccessAllowed, true)
		old := guestUserName
		guestUserName = "bob"
		defer func() { guestUserName = old }()
		code, s := getStatus(t, ggr, "alice")
		if code != http.StatusOK || s["total"] != 2.0 {
			t.Errorf("unexpected response: %d %v", code, s)
		}
	})
}

func TestStatusConcurrencyLimit(t *testing.T) {
	verifyNoLeaks(t)
	old := limit
	limit = 0
	defer func() { limit = old }()
	setDuration(t, &responseTime, 100*time.Millisecond)
	var calls atomic.Int32
	s := countingSelenoid(t, 1, &calls, false)
	setHosts(t, map[string]map[string]*config.Host{"unknown": {s.Sum: s.Host}})
	ggr := NewGGR()
	defer ggr.Close()

	code, st := getStatus(t, ggr, "")
	if code != http.StatusOK || len(st) != 0 {
		t.Errorf("unexpected response: %d %v", code, st)
	}
	if calls.Load() != 0 {
		t.Errorf("host polled despite zero limit")
	}
}

func TestPingContentType(t *testing.T) {
	ggr := NewGGR()
	defer ggr.Close()
	resp, err := http.Get(ggr.URL + paths.Ping)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("unexpected content type: %s", ct)
	}
}

func TestMetrics(t *testing.T) {
	ggr := NewGGR()
	defer ggr.Close()
	resp, err := http.Get(ggr.URL + paths.Metrics)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("unexpected code: %d", resp.StatusCode)
	}
}

func TestConfigure(t *testing.T) {
	var calls atomic.Int32
	s := countingSelenoid(t, 1, &calls, false)
	dir := t.TempDir()
	writeQuota(t, dir, "alice", s)
	if err := os.WriteFile(filepath.Join(dir, "broken.xml"), []byte("<browsers"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "unreadable.xml"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	useQuotaDir(t, dir)

	lock.RLock()
	loaded := hosts
	lock.RUnlock()
	if len(loaded) != 1 || len(loaded["alice"]) != 1 || loaded["alice"][s.Sum] == nil {
		t.Fatalf("unexpected quota: %v", loaded)
	}

	quotaDir = t.TempDir()
	if err := configure(); err == nil {
		t.Error("expected error for empty quota directory")
	}
	lock.RLock()
	defer lock.RUnlock()
	if len(hosts) != 1 || hosts["alice"] == nil {
		t.Errorf("failed reload replaced quota: %v", hosts)
	}
}
