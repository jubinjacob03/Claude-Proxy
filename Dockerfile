FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.appVersion=docker" -o /out/claude-proxy .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates wget && adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/claude-proxy /app/claude-proxy
USER app
EXPOSE 3001
ENV HOST=0.0.0.0 \
    PORT=3001 \
    UPSTREAM_BASE_URL=https://gorouter.app \
    UPSTREAM_FORMAT=anthropic \
    DEFAULT_MODEL=claude-opus-4-8
ENTRYPOINT ["/app/claude-proxy"]
CMD ["serve"]
