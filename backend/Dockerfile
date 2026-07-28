FROM golang:1.17-alpine AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend ./
RUN CGO_ENABLED=0 go build -o /out/core-server ./cmd/core-server
RUN CGO_ENABLED=0 go build -o /out/migrate ./cmd/migrate

FROM alpine:3.20
RUN addgroup -S mmdash && adduser -S mmdash -G mmdash
WORKDIR /app
COPY --from=build /out/core-server /usr/local/bin/core-server
COPY --from=build /out/migrate /usr/local/bin/migrate
COPY backend/migrations ./migrations
COPY contracts/openapi/core.yaml ./contracts/openapi/core.yaml
USER mmdash
EXPOSE 8080
CMD ["core-server"]
