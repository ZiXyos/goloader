package container

import (
	"log/slog"
	"time"
)

type Option interface {
	apply(m *Manager)
}

type optionFunc func(*Manager)

func (f optionFunc) apply(m *Manager) { f(m) }

func WithLogger(l *slog.Logger) Option {
	return optionFunc(func(m *Manager) {
		m.logger = l
	})
}

func WithConfig(cfg *Config) Option {
	return optionFunc(func(m *Manager) {
		m.cfg = cfg
	})
}

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

func WithHealthInterval(interval time.Duration) Option {
	return optionFunc(func(m *Manager) {
		m.healthCheckInterval = interval
	})
}
