//go:build linux

package ebpf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"time"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
)

// ExecveEvent is what gets stored and returned via the API.
type ExecveEvent struct {
	PID       uint32    `json:"pid"`
	Comm      string    `json:"comm"`
	Syscall   string    `json:"syscall"`
	Timestamp time.Time `json:"timestamp"`
}

// rawEvent must match struct event_t in execve.bpf.c byte-for-byte.
type rawEvent struct {
	PID       uint32
	Comm      [16]byte
	Timestamp uint64
}

// Tracer loads and runs the eBPF execve tracer.
type Tracer struct {
	objs  execveObjects
	tp    link.Link
	rd    *ringbuf.Reader
	store *Store
	stopc chan struct{}
}

// New loads the eBPF objects, attaches to the tracepoint, and returns a ready Tracer.
func New(store *Store) (*Tracer, error) {
	// Required on kernels < 5.11 to allow BPF programs to lock memory.
	if err := rlimit.RemoveMemlock(); err != nil {
		return nil, fmt.Errorf("ebpf: remove memlock: %w", err)
	}

	var objs execveObjects
	if err := loadExecveObjects(&objs, nil); err != nil {
		return nil, fmt.Errorf("ebpf: load objects: %w", err)
	}

	tp, err := link.Tracepoint("syscalls", "sys_enter_execve", objs.TraceExecve, nil)
	if err != nil {
		objs.Close()
		return nil, fmt.Errorf("ebpf: attach tracepoint: %w", err)
	}

	rd, err := ringbuf.NewReader(objs.Events)
	if err != nil {
		tp.Close()
		objs.Close()
		return nil, fmt.Errorf("ebpf: ringbuf reader: %w", err)
	}

	return &Tracer{
		objs:  objs,
		tp:    tp,
		rd:    rd,
		store: store,
		stopc: make(chan struct{}),
	}, nil
}

// Run reads from the ring buffer and feeds events into the store.
// Call in a goroutine. Blocks until Close() is called.
func (t *Tracer) Run() {
	var raw rawEvent

	for {
		record, err := t.rd.Read()
		if err != nil {
			select {
			case <-t.stopc:
				return
			default:
				log.Printf("ebpf: ringbuf read error: %v", err)
				continue
			}
		}

		if err := binary.Read(
			bytes.NewReader(record.RawSample),
			binary.LittleEndian,
			&raw,
		); err != nil {
			log.Printf("ebpf: decode error: %v", err)
			continue
		}

		comm := string(bytes.TrimRight(raw.Comm[:], "\x00"))

		t.store.Add(ExecveEvent{
			PID:       raw.PID,
			Comm:      comm,
			Syscall:   "execve",
			Timestamp: time.Now().UTC(),
		})
	}
}

// Close stops the tracer and releases all kernel resources.
func (t *Tracer) Close() {
	close(t.stopc)
	t.rd.Close()
	t.tp.Close()
	t.objs.Close()
}
