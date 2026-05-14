//go:build ignore

#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>

typedef unsigned int  u32;
typedef unsigned long long u64;
typedef unsigned char u8;

/* Must match rawEvent in tracer.go exactly (byte layout) */
struct event_t {
    u64 timestamp;
    u32 pid;
    u8 comm[16];
    u8 _pad[4];
};

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1 << 18); /* 256 KB */
} events SEC(".maps");

SEC("tracepoint/syscalls/sys_enter_execve")
int trace_execve(void *ctx)
{
    struct event_t *e = bpf_ringbuf_reserve(&events, sizeof(*e), 0);
    if (!e)
        return 0;

    u64 pid_tgid = bpf_get_current_pid_tgid();
    e->pid       = (u32)(pid_tgid >> 32);
    e->timestamp = bpf_ktime_get_ns();
    bpf_get_current_comm(&e->comm, sizeof(e->comm));

    bpf_ringbuf_submit(e, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";
