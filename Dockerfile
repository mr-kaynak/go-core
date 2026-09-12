# --platform=$BUILDPLATFORM keeps the build stage native (no emulation) while
# TARGETOS/TARGETARCH cross-compile the binaries for the requested platform,
# so linux/arm64 images actually contain arm64 executables.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine@sha256:ce864e7223ac17b1775e6fd0b4c0db580c2eb50e7953a427916379e4b92a1628 AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH sh scripts/check-runtime-crypto.sh
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w" -o /app-api ./cmd/api && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w" -o /app-grpc ./cmd/grpc && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w" -o /app-migrate ./cmd/migrate

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS base

# The base tag can lag security patches in its stable Alpine repositories.
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates tzdata
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
WORKDIR /app

# configs/ is in every image because every binary's configuration loader
# searches ./configs for config.yaml and config.$APP_ENV.yaml. A deployment
# that keeps database settings there and a migration image that could not read
# them would have the job and the server disagree about which database they
# address — silently, since the loader falls back to defaults rather than
# failing.
COPY configs/ ./configs/

# docs/ is the OpenAPI document served at /docs. Only the API server opens it.
FROM base AS api
COPY docs/ ./docs/
COPY --from=builder /app-api ./app
USER appuser
EXPOSE 3000
ENTRYPOINT ["./app"]

FROM base AS grpc
COPY --from=builder /app-grpc ./app
USER appuser
EXPOSE 50051
ENTRYPOINT ["./app"]

# The migration image carries the binary and nothing else. The SQL is embedded
# in it — copying coremigrations/ in would not be read, and would make
# `migrate create` appear to work here, writing a file into a layer that is
# discarded when the container exits.
FROM base AS migrate
COPY --from=builder /app-migrate ./app
USER appuser
ENTRYPOINT ["./app"]
