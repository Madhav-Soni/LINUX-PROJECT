# Build stage — needs clang for go generate + BPF compilation
FROM --platform=linux/amd64 golang:1.21-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    clang \
    llvm \
    libbpf-dev \
    linux-headers-amd64 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Generate eBPF Go bindings from C source
RUN go generate ./user-space/internal/ebpf/

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o fis ./user-space/cmd/fis

# ---------------------------------------------------------------
# Runtime stage
FROM --platform=linux/amd64 debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    libbpf1 \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/fis /usr/local/bin/fis
COPY --from=builder /app/user-space/configs /configs

EXPOSE 8090

ENTRYPOINT ["/usr/local/bin/fis"]
CMD ["-config", "/configs/fis.json", "-http", ":8090"]
