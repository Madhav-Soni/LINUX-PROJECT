package ebpf

// Compiles execve.bpf.c → execve_bpfel.go + execve_bpfeb.go (little/big endian)
// Run: go generate ./internal/ebpf/
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" execve ./execve.bpf.c -- -I/usr/include/bpf -I/usr/include
