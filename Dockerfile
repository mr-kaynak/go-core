# --platform=$BUILDPLATFORM keeps the build stage native (no emulation) while
# TARGETOS/TARGETARCH cross-compile the binaries for the requested platform,
# so linux/arm64 images actually contain arm64 executables.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w" -o /app-api ./cmd/api && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w" -o /app-grpc ./cmd/grpc && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w" -o /app-migrate ./cmd/migrate

FROM alpine:3.24 AS base

RUN apk add --no-cache ca-certificates tzdata
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
WORKDIR /app
COPY configs/ ./configs/
COPY coremigrations/ ./coremigrations/
COPY docs/ ./docs/

FROM base AS api
COPY --from=builder /app-api ./app
USER appuser
EXPOSE 3000
ENTRYPOINT ["./app"]

FROM base AS grpc
COPY --from=builder /app-grpc ./app
USER appuser
EXPOSE 50051
ENTRYPOINT ["./app"]

FROM base AS migrate
COPY --from=builder /app-migrate ./app
USER appuser
ENTRYPOINT ["./app"]
