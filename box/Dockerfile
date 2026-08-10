FROM golang:1.26-alpine AS build
WORKDIR /src
COPY box/go.mod ./go.mod
COPY box/ ./
RUN CGO_ENABLED=0 go build -o /out/mmdash-box ./cmd/mmdash-box

FROM alpine:3.20
RUN apk add --no-cache ca-certificates docker-cli \
    && addgroup -S mmdash \
    && adduser -S mmdash -G mmdash \
    && mkdir -p /var/lib/mmdash-box /workspace \
    && chown -R mmdash:mmdash /var/lib/mmdash-box /workspace
WORKDIR /app
COPY --from=build /out/mmdash-box /usr/local/bin/mmdash-box
USER mmdash
VOLUME ["/var/lib/mmdash-box", "/workspace"]
ENTRYPOINT ["/usr/local/bin/mmdash-box"]
