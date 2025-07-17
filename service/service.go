package serviceloader

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type Service interface {
  Name() string
  Run(ctx context.Context) error
  Stop(ctx context.Context) error
  SetServiceID(serviceID string)
}

type Config struct {
  ShutdownTimeout time.Duration  `default:"10s"     koanf:"shutdownTimeout"`
}

type Application struct {
  conf Config
  name string
  version string
  id  string

  logger *slog.Logger
  services []Service
  running chan bool
  stop chan os.Signal
}

// new create a new service instance
func New(opts ...Option) *Application {
  app := &Application{
    conf: Config{},
    stop: make(chan os.Signal, 1),
    running: make(chan bool, 1),
  }
  for _, opt := range opts {
    opt(app)
  }
  return app
}

// Run run each service in a separate goroutine
func (a* Application) Run() {
  ctx := context.Background();
  for _, service := range a.services {
    service.SetServiceID(a.id)

    go func() {
      if err := service.Run(ctx); err != nil {
        a.logger.Error(
          "failed to start service id:",
          slog.String("service", service.Name()),
          slog.String("err", err.Error()),
          )
        a.stop <- syscall.SIGTERM
      }
    }()
  }

  signal.Notify(a.stop, syscall.SIGINT, syscall.SIGTERM)
  a.running <- true
  <-a.stop

  stopCtx, cancel := context.WithTimeout(ctx, a.conf.ShutdownTimeout)
  defer cancel()

  if err := a.shutdown(stopCtx); err != nil {
    a.logger.Error("failed to shutdown one or more services", slog.String("err", err.Error()))
  }

  select {
  case <- stopCtx.Done():
    a.logger.Warn("failed to shutdown services before timeout")
  default:
    a.logger.Info("service shutdown")
}

}

func (a *Application) shutdown(ctx context.Context) error {
  var err error

  for _, service := range a.services {
    if serviceErr := service.Stop(ctx); serviceErr != nil {
      err = errors.Join(err, fmt.Errorf("%s shutdown: %w", service.Name(), serviceErr))
    }
  }

  return err
}
