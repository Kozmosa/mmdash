FROM golang:1.26-alpine AS build
ENV GOPROXY=https://goproxy.cn
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend ./
RUN CGO_ENABLED=0 go build -o /out/core-server ./cmd/core-server
RUN CGO_ENABLED=0 go build -o /out/migrate ./cmd/migrate
RUN CGO_ENABLED=0 go build -o /out/artifact-storage-init ./cmd/artifact-storage-init
RUN CGO_ENABLED=0 go build -o /out/mmdash-git-askpass ./cmd/mmdash-git-askpass

FROM alpine:3.20
RUN apk add --no-cache ca-certificates git \
    && addgroup -S mmdash \
    && adduser -S mmdash -G mmdash \
    && mkdir -p /var/lib/mmdash/artifacts /var/lib/mmdash/repos \
    && chown -R mmdash:mmdash /var/lib/mmdash
WORKDIR /app
COPY --from=build /out/core-server /usr/local/bin/core-server
COPY --from=build /out/migrate /usr/local/bin/migrate
COPY --from=build /out/artifact-storage-init /usr/local/bin/artifact-storage-init
COPY --from=build /out/mmdash-git-askpass /usr/local/bin/mmdash-git-askpass
COPY backend/migrations ./migrations
COPY contracts/openapi/core.yaml ./contracts/openapi/core.yaml
USER mmdash
VOLUME ["/var/lib/mmdash/artifacts", "/var/lib/mmdash/repos"]
EXPOSE 8080
CMD ["core-server"]
