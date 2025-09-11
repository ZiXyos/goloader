package serviceloader

import (
	"fmt"
	"log/slog"
)

type Option func(*Application)

func WithIdentifier(name string, version string) Option {
  return func(a *Application) {
    a.name = name
    a.version = version
    a.id = fmt.Sprintf("%s/%s", name, version)
  }
}

func WithConfig(config Config) Option {
  return func(a *Application) {
    a.conf = config
  }
}

func WithService(services ...Service) Option {
  return func(a *Application) {
    a.services = services
  }
}

func WithLogger(logger slog.Logger) Option {
  return func(a *Application) {
    a.logger = logger
  }
}
