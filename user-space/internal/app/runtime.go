package app

import (
	"sync"

	"github.com/owais/fis/user-space/internal/config"
	"github.com/owais/fis/user-space/internal/isolation"
	"github.com/owais/fis/user-space/internal/logger"
	"github.com/owais/fis/user-space/internal/policy"
	"github.com/owais/fis/user-space/internal/recovery"
)

type Runtime struct {
	mu       sync.RWMutex
	cfg      config.Config
	engine   *policy.Engine
	cgroups  *isolation.Manager
	recovery *recovery.Engine
	log      *logger.Logger
	observer recovery.ActionObserver
}

func NewRuntime(cfg config.Config, log *logger.Logger, observer recovery.ActionObserver) (*Runtime, error) {
	engine, cgroups, rec, err := buildComponents(cfg, log, observer)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		cfg:      cfg,
		engine:   engine,
		cgroups:  cgroups,
		recovery: rec,
		log:      log,
		observer: observer,
	}, nil
}

func (r *Runtime) Build(cfg config.Config) (*policy.Engine, *isolation.Manager, *recovery.Engine, error) {
	return buildComponents(cfg, r.log, r.observer)
}

func (r *Runtime) Swap(cfg config.Config, engine *policy.Engine, cgroups *isolation.Manager, rec *recovery.Engine) {
	r.mu.Lock()
	r.cfg = cfg
	r.engine = engine
	r.cgroups = cgroups
	r.recovery = rec
	r.mu.Unlock()
}

func (r *Runtime) Snapshot() (config.Config, *policy.Engine, *recovery.Engine) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cfg, r.engine, r.recovery
}

func (r *Runtime) Config() config.Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cfg
}

func (r *Runtime) Cleanup() {
	r.mu.RLock()
	cgroups := r.cgroups
	r.mu.RUnlock()
	if cgroups != nil {
		cgroups.Cleanup()
	}
}

func buildComponents(cfg config.Config, log *logger.Logger, observer recovery.ActionObserver) (*policy.Engine, *isolation.Manager, *recovery.Engine, error) {
	engine, err := policy.NewEngine(cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	cgroups := isolation.NewManager(cfg.CgroupRoot, log)
	rec := recovery.New(log, cgroups, observer)
	return engine, cgroups, rec, nil
}
