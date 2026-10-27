// package container contains the container implementation.
package container

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"syscall"
	"time"
)

type Component interface {
	Open(ctx context.Context) error
	Close(ctx context.Context) error
	Name() string
	isOptional() bool
}

type Healthchecker interface {
	Check(ctx context.Context) error
}

type Config struct {
	ShutdownTimeout     time.Duration `default:"10s" koanf:"shutdown_timeout"`
	HealthCheckInterval time.Duration `default:"10s" koanf:"health_check_interval"`
}

type Manager struct {
	logger *slog.Logger
	cfg    *Config

	stop    chan os.Signal
	running chan bool

	components []Component

	wg                  *sync.WaitGroup
	healthCheckInterval time.Duration
}

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

func (m *Manager) Optional(ctx context.Context) error {
	return nil
}

func (m *Manager) Open(ctx context.Context) error {
	for _, component := range m.components {
		m.wg.Add(1)
		go func(cmp Component) {
			defer m.wg.Done()
			if err := cmp.Open(ctx); err != nil {
				m.logger.WarnContext(
					ctx,
					"failed to ping component",
					slog.String("component", cmp.Name()),
					slog.String("error", err.Error()),
				)

				m.stop <- syscall.SIGTERM
			}
		}(component)
	}

	m.running <- true
	<-m.stop

	stopCtx, cancel := context.WithTimeout(ctx, m.cfg.ShutdownTimeout)
	defer cancel()

	if err := m.shutdown(stopCtx); err != nil {
		m.logger.Error(
			"failed to shutdown one or more component",
			slog.String("error", err.Error()),
		)
	}

	stopCtx.Done()
	m.logger.Info("component killed")
	return nil
}

func (m *Manager) shutdown(ctx context.Context) error {
	var err error

	for _, component := range m.components {
		if componentErr := component.Close(ctx); componentErr != nil {
			m.logger.WarnContext(ctx, "component failed to close", "component_name", component.Name, "error", componentErr.Error())
			err = errors.Join(err, fmt.Errorf("%s close: %w", component.Name(), componentErr))
		}
	}

	return nil
}
