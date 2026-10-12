FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build

ARG TARGETOS
ARG TARGETARCH
ARG GIT_REVISION=HEAD
ARG BUILD_STAMP=unknown

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X main.gitRevision=${GIT_REVISION} -X main.buildStamp=${BUILD_STAMP}" \
    -o /out/ggr-ui .

FROM alpine:3

RUN apk add --no-cache tzdata ca-certificates
COPY --from=build /out/ggr-ui /usr/bin/ggr-ui

USER 65534:65534
EXPOSE 8888
ENTRYPOINT ["/usr/bin/ggr-ui", "-quota-dir", "/etc/grid-router/quota"]
