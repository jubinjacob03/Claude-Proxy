FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO stays off: the SQLite driver is pure Go, so the binaries keep working on a
# slim runtime without a matching libc.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/claude-relay ./cmd/claude-relay

FROM debian:bookworm-slim AS runtime

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=build /out/claude-relay /app/claude-relay

# The relay holds licence and account secrets, so it does not run as root.
RUN useradd --create-home --uid 10001 relay && \
    mkdir -p /data && chown -R relay:relay /data /app
USER relay

ENV RELAY_ADDR=:8080 \
    RELAY_DATA_DIR=/data

EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/app/claude-relay"]
