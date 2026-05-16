//go:build !linux

package ebpf

type Store struct{}
func NewStore() *Store { return &Store{} }

type ExecveEvent struct{}

type Tracer struct{}
func New(store *Store) (*Tracer, error) { return &Tracer{}, nil }
func (t *Tracer) Run() {}
func (t *Tracer) Close() error { return nil }
