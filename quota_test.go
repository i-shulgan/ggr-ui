package main

import (
	"context"
	"crypto/md5"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sessionsHandler emulates Selenoid /status with a single running session.
func sessionsHandler(sessionID string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"total":5,"used":1,"queued":0,"pending":0,"browsers":{"chrome":{"120.0":{"test":{"count":1,"sessions":[{"id":%q,"container":"c","caps":{"browserName":"chrome"}}]}}}}}`, sessionID)
	})
}

func writeQuota(t *testing.T, dir, user string, hosts ...*Selenoid) {
	t.Helper()
	var b strings.Builder
	b.WriteString(`<qa:browsers xmlns:qa="urn:config.gridrouter.qatools.ru"><browser name="chrome" defaultVersion="120.0"><version number="120.0"><region name="1">`)
	for _, h := range hosts {
		fmt.Fprintf(&b, `<host name="%s" port="%d" count="5"/>`, h.Host.Name, h.Host.Port)
	}
	b.WriteString(`</region></version></browser></qa:browsers>`)
	if err := os.WriteFile(filepath.Join(dir, user+".xml"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func useQuotaDir(t *testing.T, dir string) {
	t.Helper()
	old := quotaDir
	quotaDir = dir
	t.Cleanup(func() { quotaDir = old })
	if err := configure(); err != nil {
		t.Fatal(err)
	}
}

func sessionIDs(s Status) []string {
	var ids []string
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch v := v.(type) {
		case map[string]interface{}:
			if id, ok := v["id"].(string); ok {
				ids = append(ids, id)
			}
			for _, vv := range v {
				walk(vv)
			}
		case []interface{}:
			for _, vv := range v {
				walk(vv)
			}
		}
	}
	walk(map[string]interface{}(s))
	return ids
}

// Regression: with pre-Go 1.22 loop semantics every host of a region pointed
// to the last one, so only the last host was polled under all prefixes.
func TestQuotaSeveralHostsInRegion(t *testing.T) {
	first := NewSelenoid(sessionsHandler("first"))
	defer first.Close()
	second := NewSelenoid(sessionsHandler("second"))
	defer second.Close()

	dir := t.TempDir()
	writeQuota(t, dir, "test", first, second)
	useQuotaDir(t, dir)

	lock.RLock()
	quota := hosts["test"]
	lock.RUnlock()
	if len(quota) != 2 {
		t.Fatalf("expected 2 hosts in quota, got %d", len(quota))
	}
	for sum, h := range quota {
		if h.Sum() != sum {
			t.Errorf("host %s is stored under foreign key %s", h.Route(), sum)
		}
	}

	ggr := NewGGR()
	defer ggr.Close()
	s, err := ggr.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s["used"] != 2.0 {
		t.Errorf("unexpected used: %v", s["used"])
	}
	ids := sessionIDs(s)
	want := map[string]int{
		fmt.Sprintf("%x", md5.Sum([]byte(first.URL))) + "first":   0,
		fmt.Sprintf("%x", md5.Sum([]byte(second.URL))) + "second": 0,
	}
	for _, id := range ids {
		if _, ok := want[id]; !ok {
			t.Errorf("unexpected session id: %s", id)
			continue
		}
		want[id]++
	}
	for id, n := range want {
		if n != 1 {
			t.Errorf("session %s seen %d times, want 1", id, n)
		}
	}
}
