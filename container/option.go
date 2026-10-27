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

func WithComponent(c ...Component) Option {
	return optionFunc(func(m *Manager) {
		m.components = c
	})
}

func WithHealthInterval(interval time.Duration) Option {
	return optionFunc(func(m *Manager) {
		m.healthCheckInterval = interval
	})
}
