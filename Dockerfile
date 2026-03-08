# syntax=docker/dockerfile:1

FROM node:20-bookworm AS dashboard-builder
WORKDIR /src/internal/dashboard/web

COPY internal/dashboard/web/package*.json ./
RUN npm ci

COPY internal/dashboard/web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS builder
ARG TARGETOS=linux
ARG TARGETARCH
WORKDIR /src

# go-sqlite3 requires CGO-enabled builds.
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    git \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=dashboard-builder /src/internal/dashboard/dist /src/internal/dashboard/dist
RUN CGO_ENABLED=1 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/foreman ./main.go

FROM debian:bookworm-slim AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    tini \
    git \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /out/foreman /usr/local/bin/foreman

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["foreman", "doctor", "--quick"]

ENTRYPOINT ["/usr/bin/tini", "--", "foreman"]
CMD ["start"]
