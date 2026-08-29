FROM --platform=$BUILDPLATFORM golang:1.24-bookworm AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/lifelog ./cmd/lifelog

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /out/lifelog /usr/local/bin/lifelog

ENV LIFELOG_DATA_DIR=/data \
    LIFELOG_ADDR=:8080
VOLUME ["/data"]
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/lifelog"]
