# syntax=docker/dockerfile:1
# Containerfile — works with both podman and docker.
#
# Stages:
#   builder  — compiles the binary
#   test     — runs all unit tests (used by CI; never shipped)
#   final    — minimal distroless image containing only the binary

# ─── builder ────────────────────────────────────────────────────────────────
FROM golang:1.25.8-alpine AS builder

WORKDIR /src

# Download dependencies before copying source (improves layer caching).
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
      -trimpath \
      -ldflags="-w -s" \
      -o /bin/platform-runner \
      .

# ─── test ────────────────────────────────────────────────────────────────────
# This stage is only used in CI to run tests inside the same build environment.
# It is never pushed or run in production.
FROM builder AS test

RUN go test ./... -count=1 -race

# ─── final ───────────────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot AS final

COPY --from=builder /bin/platform-runner /bin/platform-runner

ENTRYPOINT ["/bin/platform-runner"]
