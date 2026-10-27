package container

import (
	"log/slog"
	"time"
)

// Option configures a Manager during construction. It is an interface rather
// than a bare function type so that options stay comparable in tests and can
// grow behaviour later without breaking callers.
type Option interface {
	apply(m *Manager)
}

type optionFunc func(*Manager)

func (f optionFunc) apply(m *Manager) { f(m) }

// WithLogger sets the structured logger used by the Manager. It is required:
// New fails when no logger is provided.
func WithLogger(l *slog.Logger) Option {
	return optionFunc(func(m *Manager) {
		m.logger = l
	})
}

// WithConfig sets the Manager configuration (shutdown timeout, health check
// interval). It is required: New fails when no configuration is provided.
func WithConfig(cfg *Config) Option {
	return optionFunc(func(m *Manager) {
		m.cfg = cfg
	})
}

// WithComponent registers components to be managed. It can be passed more than
// once; components accumulate, and their registration order is the order in
// which they are opened. Components wrapped with Optional are unwrapped and
// marked as non-critical.
func WithComponent(cmp ...Component) Option {
	return optionFunc(func(m *Manager) {
		for _, c := range cmp {
			e := &entry{component: c}
			if o, ok := c.(optional); ok {
				e.component, e.optional = o.Component, true
			}

			m.entries = append(m.entries, e)
		}
	})
}

// WithHealthInterval sets how often the Manager checks the health of the
// components that implement Healthchecker.
func WithHealthInterval(interval time.Duration) Option {
	return optionFunc(func(m *Manager) {
		m.healthCheckInterval = interval
	})
}
