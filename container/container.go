// Package container manages the lifecycle of an application's components:
// passive dependencies such as database pools, object storage buckets or cache
// clients, as opposed to the long-running services driven by serviceloader.
//
// Components are opened in registration order before any service starts, and
// closed in reverse order once every service has stopped.
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

// Component is a dependency whose lifecycle the Manager drives.
//
// Packages implement it implicitly and never need to import container. Its
// constructor must not perform I/O, so it can be wired before Open is called.
//
// Open performs the I/O needed to make the component usable, such as dialing
// or pinging.
type Component interface {
	// Open perform the I/O needed to make the component usable such as dialing
	// or pinging.
	Open(ctx context.Context) error

	// Close releases its resources; it must be safe to call even when
	// Open failed or was never called.
	Close(ctx context.Context) error

	// Name identifies the component in logs and errors.
	Name() string
}

// Healthchecker is implemented by components that can report their health.
type Healthchecker interface {
	// Check returns nil when the component is usable and the reason otherwise.
	Check(ctx context.Context) error
}

// Config holds the Manager settings.
type Config struct {
	// ShutdownTimeout  bounds the time given to Close,
	ShutdownTimeout time.Duration `default:"10s" koanf:"shutdown_timeout"`

	// HealthCheckInterval paces the health checks.
	HealthCheckInterval time.Duration `default:"10s" koanf:"health_check_interval"`
}

type entry struct {
	component Component

	optional  bool
	attempted bool

	healthy atomic.Bool
}

// Manager opens, health-checks and closes a set of components. The zero value
// is not usable; build one with New.
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

// New builds a Manager from opts. It returns an error when the logger or the
// configuration is missing, so a caller never receives a partially
// initialised Manager.
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

// Optional marks o as non-critical. When it fails to open, the Manager logs a
// warning and keeps going instead of failing the whole Open; the component is
// reported unhealthy and is still closed by Close.
//
// The returned value is only a marker: WithComponent unwraps it, so the
// Manager always calls the original component directly.
func (m *Manager) Optional(o Component) Component {
	return optional{o}
}

// Open opens every component sequentially, in registration order, and returns
// once all of them are open.
//
// When a required component fails, Open closes the components it has already
// attempted, in reverse order and including the failing one, then returns the
// open error joined with any close errors. A failing optional component is
// logged and skipped.
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

// Close closes every component that Open attempted, in reverse registration
// order, within the configured ShutdownTimeout. Every component is closed even
// when an earlier one fails; the failures are joined in the returned error.
//
// Call it only once every service using the components has stopped.
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
			errs = errors.Join(errs, fmt.Errorf("close %s: %w", e.component.Name(), err))
		}
	}

	return errs
}
