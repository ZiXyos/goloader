// Package serviceloader runs long-lived services (HTTP and gRPC servers,
// workers) as one unit: it starts them, waits for a termination signal or a
// service failure, then stops them and waits for them to finish.
package serviceloader

//go:generate mockery

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// _defaultShutdownTimeout bounds the shutdown when the configuration does not
// set one. A zero timeout would hand Stop an already expired context.
const _defaultShutdownTimeout = 10 * time.Second

// UUID is the identifier given to every service run by an Application.
type UUID [16]byte

// Generator produces service identifiers.
type Generator func() UUID

// Service represents a long-lived process run by an Application.
//
// Run starts the service. It may return as soon as the service is running or
// block until the service ends; a non-nil error is treated as a failure and
// shuts the whole Application down.
//
// Stop gracefully stops the service. It must return only once the service's
// in-flight work (requests, jobs) is finished, because the Application treats
// a returned Stop as the point where the service's dependencies can be closed.
type Service interface {
	Name() string
	Run(ctx context.Context) error
	Stop(ctx context.Context) error
	SetServiceID(serviceID UUID)
}

// Config represents the application configuration settings.
type Config struct {
	ShutdownTimeout time.Duration `default:"10s" koanf:"shutdownTimeout"`
}

// Application orchestrates a set of services. The zero value is not usable;
// build one with New.
type Application struct {
	conf Config

	id      string
	name    string
	version string

	logger *slog.Logger

	services []Service

	generator Generator
}

// New creates a new Application. Missing options fall back to defaults: the
// default slog logger, a random UUID generator, and a 10s shutdown timeout.
func New(opts ...Option) *Application {
	app := &Application{}
	for _, opt := range opts {
		opt(app)
	}

	if app.logger == nil {
		app.logger = slog.Default()
	}

	if app.generator == nil {
		app.generator = randomUUID
	}

	if app.conf.ShutdownTimeout <= 0 {
		app.conf.ShutdownTimeout = _defaultShutdownTimeout
	}

	return app
}

// Run starts every service in its own goroutine and blocks until ctx is
// cancelled, the process receives SIGINT or SIGTERM, or a service fails.
//
// It then stops the services in reverse registration order and waits for
// their Run calls to return, all within the configured shutdown timeout. When
// Run returns, no service is running anymore, so the caller can safely close
// the services' dependencies.
//
// The returned error joins the service failure that triggered the shutdown, if
// any, with every error raised while shutting down.
func (a *Application) Run(ctx context.Context) error {
	sigCtx, stopSignals := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	// failCtx is cancelled by the first failing service. Services themselves
	// receive ctx rather than failCtx: shutdown is driven by Stop, so that a
	// signal never aborts in-flight work before Stop gets to drain it.
	failCtx, fail := context.WithCancelCause(sigCtx)
	defer fail(nil)

	var wg sync.WaitGroup

	for _, svc := range a.services {
		wg.Add(1)

		go func() {
			defer wg.Done()

			svc.SetServiceID(a.generator())

			if err := svc.Run(ctx); err != nil {
				a.logger.ErrorContext(ctx, "service failed",
					slog.String("service", svc.Name()),
					slog.Any("error", err),
				)

				fail(fmt.Errorf("run %s: %w", svc.Name(), err))
			}
		}()
	}

	<-failCtx.Done()

	var runErr error
	if cause := context.Cause(failCtx); !errors.Is(cause, context.Canceled) {
		runErr = cause
	}

	a.logger.InfoContext(ctx, "shutting down services", slog.String("application", a.id))

	// The shutdown gets a fresh deadline: ctx may already be cancelled, which
	// is often the very reason the shutdown started.
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.conf.ShutdownTimeout)
	defer cancel()

	shutdownErr := errors.Join(a.shutdown(stopCtx), wait(stopCtx, &wg))

	if shutdownErr != nil {
		a.logger.ErrorContext(stopCtx, "failed to shut down one or more services",
			slog.Any("error", shutdownErr),
		)
	} else {
		a.logger.InfoContext(stopCtx, "services shut down", slog.String("application", a.id))
	}

	return errors.Join(runErr, shutdownErr)
}

// shutdown stops the services in reverse registration order, so a service
// registered after another, and possibly depending on it, stops first. Every
// service is stopped even when an earlier one fails.
func (a *Application) shutdown(ctx context.Context) error {
	var errs error

	for i := len(a.services) - 1; i >= 0; i-- {
		svc := a.services[i]

		if err := svc.Stop(ctx); err != nil {
			a.logger.WarnContext(ctx, "service failed to stop",
				slog.String("service", svc.Name()),
				slog.Any("error", err),
			)

			errs = errors.Join(errs, fmt.Errorf("stop %s: %w", svc.Name(), err))
		}
	}

	return errs
}

// wait blocks until every service's Run has returned or ctx expires. On
// expiry the stuck goroutines are left behind: there is no way to kill them,
// and the caller is about to exit anyway.
func wait(ctx context.Context, wg *sync.WaitGroup) error {
	done := make(chan struct{})

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for services to return: %w", ctx.Err())
	}
}

// randomUUID returns a random (version 4) UUID.
func randomUUID() UUID {
	var id UUID

	// crypto/rand.Read never returns an error on supported platforms.
	_, _ = rand.Read(id[:])

	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80

	return id
}
