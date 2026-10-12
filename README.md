# Ggr UI
[![ci](https://github.com/i-shulgan/ggr-ui/actions/workflows/ci.yml/badge.svg)](https://github.com/i-shulgan/ggr-ui/actions/workflows/ci.yml)

Ggr UI is a standalone daemon that automatically collects `/status` information from multiple [Selenoid](https://github.com/aerokube/selenoid) instances behind [Ggr](https://github.com/aerokube/ggr) and returns it as a single `/status` API. It also proxies VNC (`/vnc/`) and log (`/logs/`) websockets to the right Selenoid host. With this daemon running, [Selenoid UI](https://github.com/aerokube/selenoid-ui) shows the state of the entire cluster.

This is a maintained fork of [aerokube/ggr-ui](https://github.com/aerokube/ggr-ui), which is unmaintained since 1.2.0.

## Differences from upstream

Fixes:

* **Quota with several hosts in a region.** Upstream stored a pointer to the loop variable for every host, so all hosts of a region pointed to the last one: `/status` polled only that host, showed its sessions under every host prefix, and over-counted `used`.
* **Goroutine leaks in `/status`.** When `-response-time` was exceeded or the client disconnected, the goroutines polling hosts blocked forever.
* **Quota reload no longer waits for slow hosts.** `/status` held the quota lock during network calls, so `SIGHUP` reload could hang for up to `-timeout`.
* **Websocket proxy** uses `wss://` for hosts with `scheme="https"` and sends the host `username`/`password` from quota as basic auth.
* Signals are no longer dropped (`signal.Notify` with buffered channels); `go vet` and `staticcheck` are clean.

Build and delivery:

* Go 1.27.2, dependencies updated, `govulncheck` in CI.
* Multi-stage `Dockerfile`: static binary, `alpine` runtime, runs as `nobody` (uid/gid 65534).
* Images are published to `ghcr.io/i-shulgan/ggr-ui` instead of Docker Hub. No binary release assets and no gh-pages docs.

`/video/` is intentionally not proxied: Selenoid UI shows the video tab only when `<webdriver-uri>/video?json` returns a list, and Ggr cannot list videos across hosts, so the tab never appears in a Ggr setup. Individual recordings can be downloaded through Ggr itself at `/video/<session-id>`.

## Running

```bash
docker run -d --name ggr-ui -p 8888:8888 \
    -v /etc/grid-router/quota:/etc/grid-router/quota:ro \
    ghcr.io/i-shulgan/ggr-ui:latest
```

The container runs as uid 65534, so quota files must be world-readable. Send `SIGHUP` to reload quota: `docker kill -s HUP ggr-ui`. Flags are described in the [upstream guide](http://aerokube.com/ggr-ui/latest/) and in `ggr-ui --help`.

Image tags:

| Tag | Source |
|-----|--------|
| `latest` | last commit to `master` |
| `sha-<short-sha>` | a specific commit to `master` |
| `<tag>` | a pushed git tag, e.g. `1.3.0` |

## Building

Image:

```bash
docker build \
    --build-arg GIT_REVISION=$(git describe --tags --always) \
    --build-arg BUILD_STAMP=$(date -u +%Y-%m-%dT%H:%M:%SZ) \
    -t ggr-ui .
```

Binary (Go 1.27.2 or newer):

```bash
CGO_ENABLED=0 go build -ldflags "-X main.gitRevision=$(git describe --tags --always)"
./ggr-ui --help
```

Add `-tags watch` to also reload quota when files in `-quota-dir` change.

Checks that CI runs:

```bash
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@latest ./...
go test -race -tags watch -cover ./...
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```
