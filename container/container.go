// package container contains the container implementation.
package container

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

type Component interface {
	Open(ctx context.Context) error
	Close(ctx context.Context) error
	Name() string
}

type Healthchecker interface {
	Check(ctx context.Context) error
}

type Config struct {
	ShutdownTimeout     time.Duration `default:"10s" koanf:"shutdown_timeout"`
	HealthCheckInterval time.Duration `default:"10s" koanf:"health_check_interval"`
}

type entry struct {
	component Component

	optional  bool
	attempted bool

	healthy atomic.Bool
}

type Manager struct {
	logger *slog.Logger
	cfg    *Config

	stop    chan os.Signal
	running chan bool

	entries []*entry

	wg                  *sync.WaitGroup
	healthCheckInterval time.Duration
}

type optional struct{ Component }

func New(opts ...Option) (*Manager, error) {
	m := &Manager{
		running: make(chan bool, 1),
		stop:    make(chan os.Signal, 1),
		wg:      new(sync.WaitGroup),
	}
	for _, opt := range opts {
		opt.apply(m)
	}

	if m.logger == nil {
		return nil, errors.New("container manager: logger is required")
	}

	if m.cfg == nil {
		return nil, errors.New("container manager: config is required")
	}

	return m, nil
}

func (m *Manager) Optional(o Component) Component {
	return optional{o}
}

func (m *Manager) Open(ctx context.Context) error {
	for i, e := range m.entries {
		e.attempted = true
		err := e.component.Open(ctx)
		e.healthy.Store(err == nil)
		if err == nil {
			continue
		}

		if e.optional {
			m.logger.WarnContext(
				ctx,
				"optioan component failed to open, continuing with default fallback",
				slog.String("component", e.component.Name()),
				slog.String("error", err.Error()),
			)
			continue
		}

		openErr := fmt.Errorf("open %s: %w", e.component.Name(), err)
		closeErr := m.close(context.WithoutCancel(ctx), m.entries[:i+1])

		return errors.Join(openErr, closeErr)
	}

	return nil
}

func (m *Manager) Close(ctx context.Context) error {
	return m.close(ctx, m.entries)
}

func (m *Manager) close(ctx context.Context, entries []*entry) error {
	ctx, cancel := context.WithTimeout(ctx, m.cfg.ShutdownTimeout)
	defer cancel()

	var errs error
	for _, e := range slices.Backward(entries) {
		if !e.attempted {
			continue
		}
		e.healthy.Store(false)
		if err := e.component.Close(ctx); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close %s: %w", e.component.Name()), err)
		}
	}

	return errs
}
