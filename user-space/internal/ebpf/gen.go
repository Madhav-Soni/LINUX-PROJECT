package ebpf

// Compiles execve.bpf.c → execve_bpfel.go + execve_bpfeb.go (little/big endian)
// Run: go generate ./internal/ebpf/
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-I/usr/include/aarch64-linux-gnu" execve execve.bpf.c
