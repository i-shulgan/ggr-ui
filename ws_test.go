package main

import (
	"crypto/md5"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aerokube/ggr/config"
	"golang.org/x/net/websocket"
)

const (
	wsUser     = "selenoid"
	wsPassword = "secret"
)

// newWSBackend starts an echo websocket server that requires basic auth and
// reports the path it was called with.
func newWSBackend(t *testing.T, tlsServer bool) (*config.Host, string, <-chan string) {
	t.Helper()
	paths := make(chan string, 1)
	srv := websocket.Server{
		Handshake: func(_ *websocket.Config, r *http.Request) error {
			if u, p, ok := r.BasicAuth(); !ok || u != wsUser || p != wsPassword {
				return fmt.Errorf("unauthorized")
			}
			return nil
		},
		Handler: func(ws *websocket.Conn) {
			paths <- ws.Request().URL.Path
			_, _ = io.Copy(ws, ws)
		},
	}
	var s *httptest.Server
	if tlsServer {
		s = httptest.NewTLSServer(srv)
		old := wsTLSConfig
		wsTLSConfig = s.Client().Transport.(*http.Transport).TLSClientConfig
		t.Cleanup(func() { wsTLSConfig = old })
	} else {
		s = httptest.NewServer(srv)
	}
	t.Cleanup(s.Close)
	u, _ := url.Parse(s.URL)
	name, p, _ := net.SplitHostPort(u.Host)
	port, _ := strconv.Atoi(p)
	h := &config.Host{Name: name, Port: port, Scheme: u.Scheme, Username: wsUser, Password: wsPassword}
	return h, fmt.Sprintf("%x", md5.Sum([]byte(s.URL))), paths
}

func dialGGR(t *testing.T, ggr *GGR, path string) *websocket.Conn {
	t.Helper()
	conn, err := websocket.Dial("ws"+strings.TrimPrefix(ggr.URL, "http")+path, "", "http://localhost")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	return conn
}

func TestProxyWS(t *testing.T) {
	for _, c := range []struct {
		name  string
		tls   bool
		route string
	}{
		{"VNC", false, paths.VNC},
		{"Logs", false, paths.Logs},
		{"VNCOverTLS", true, paths.VNC},
		{"LogsOverTLS", true, paths.Logs},
	} {
		t.Run(c.name, func(t *testing.T) {
			verifyNoLeaks(t)
			host, sum, backendPaths := newWSBackend(t, c.tls)
			setHosts(t, map[string]map[string]*config.Host{"unknown": {sum: host}})
			ggr := NewGGR()
			t.Cleanup(ggr.Close)

			conn := dialGGR(t, ggr, c.route+sum+"session-id")
			if _, err := conn.Write([]byte("hello")); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 5)
			if _, err := io.ReadFull(conn, buf); err != nil {
				t.Fatalf("no echo through proxy: %v", err)
			}
			if string(buf) != "hello" {
				t.Errorf("unexpected echo: %q", buf)
			}
			if p := <-backendPaths; p != c.route+"session-id" {
				t.Errorf("unexpected backend path: %s", p)
			}
		})
	}
}

func TestProxyWSRejected(t *testing.T) {
	host, sum, _ := newWSBackend(t, false)
	down := NewSelenoid(nil)
	down.Close()
	setHosts(t, map[string]map[string]*config.Host{"unknown": {sum: host, down.Sum: down.Host}})
	ggr := NewGGR()
	defer ggr.Close()

	for name, path := range map[string]string{
		"ShortPath":   paths.VNC + "123",
		"UnknownHost": paths.VNC + fmt.Sprintf("%x", md5.Sum([]byte("missing"))) + "123",
		"HostDown":    paths.VNC + down.Sum + "123",
	} {
		t.Run(name, func(t *testing.T) {
			conn := dialGGR(t, ggr, path)
			if _, err := conn.Read(make([]byte, 1)); err != io.EOF {
				t.Errorf("expected closed connection, got %v", err)
			}
		})
	}
}

func TestProxyWSUnknownUser(t *testing.T) {
	old := authenticatedAccessOnly
	authenticatedAccessOnly = true
	defer func() { authenticatedAccessOnly = old }()
	host, sum, _ := newWSBackend(t, false)
	setHosts(t, map[string]map[string]*config.Host{"someone": {sum: host}})
	ggr := NewGGR()
	defer ggr.Close()

	conn := dialGGR(t, ggr, paths.VNC+sum+"123")
	if _, err := conn.Read(make([]byte, 1)); err != io.EOF {
		t.Errorf("expected closed connection, got %v", err)
	}
}
