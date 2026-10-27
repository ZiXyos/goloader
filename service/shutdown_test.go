package serviceloader_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	serviceloader "github.com/zixyos/goloader/service"
)

// fakeService is a Service whose behaviour is set per test. When block is
// true, Run blocks until Stop is called, like a worker or a nested
// application; otherwise it returns runErr immediately, like an HTTP server
// that listens in its own goroutine.
type fakeService struct {
	name    string
	runErr  error
	stopErr error
	block   bool
	hang    bool // Run never returns, even after Stop

	stopOnce sync.Once
	stopped  chan struct{}
	returned chan struct{}
	recorder *recorder
}

func newFake(name string, rec *recorder) *fakeService {
	return &fakeService{
		name:     name,
		stopped:  make(chan struct{}),
		returned: make(chan struct{}),
		recorder: rec,
	}
}

func (f *fakeService) Name() string                    { return f.name }
func (f *fakeService) SetServiceID(serviceloader.UUID) {}

func (f *fakeService) Run(context.Context) error {
	defer close(f.returned)

	switch {
	case f.hang:
		select {}
	case f.block:
		<-f.stopped
	}

	return f.runErr
}

func (f *fakeService) Stop(context.Context) error {
	f.stopOnce.Do(func() { close(f.stopped) })

	if f.recorder != nil {
		f.recorder.add(f.name)
	}

	return f.stopErr
}

type recorder struct {
	mu    sync.Mutex
	order []string
}

func (r *recorder) add(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.order = append(r.order, name)
}

func (r *recorder) get() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.order...)
}

func newApp(timeout time.Duration, services ...serviceloader.Service) *serviceloader.Application {
	return serviceloader.New(
		serviceloader.WithLogger(slog.New(slog.NewTextHandler(discard{}, nil))),
		serviceloader.WithConfig(serviceloader.Config{ShutdownTimeout: timeout}),
		serviceloader.WithService(services...),
	)
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// runAsync runs app and returns a channel receiving Run's error. The test
// fails if Run does not return within the deadline.
func runAsync(t *testing.T, ctx context.Context, app *serviceloader.Application) <-chan error {
	t.Helper()

	errCh := make(chan error, 1)
	go func() { errCh <- app.Run(ctx) }()

	return errCh
}

func await(t *testing.T, errCh <-chan error) error {
	t.Helper()

	select {
	case err := <-errCh:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
		return nil
	}
}

func TestRun_ContextCancelShutsDownCleanly(t *testing.T) {
	a := newFake("a", nil)
	a.block = true
	b := newFake("b", nil)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := runAsync(t, ctx, newApp(time.Second, a, b))

	cancel()

	require.NoError(t, await(t, errCh))

	select {
	case <-a.returned:
	default:
		t.Fatal("Run returned before the blocking service's Run did")
	}
}

func TestRun_ServiceFailureTriggersShutdownAndIsReturned(t *testing.T) {
	errBoom := errors.New("boom")

	healthy := newFake("healthy", nil)
	healthy.block = true
	broken := newFake("broken", nil)
	broken.runErr = errBoom

	errCh := runAsync(t, context.Background(), newApp(time.Second, healthy, broken))

	err := await(t, errCh)
	require.ErrorIs(t, err, errBoom)
	assert.Contains(t, err.Error(), "run broken")
}

func TestRun_SeveralFailuresDoNotDeadlock(t *testing.T) {
	var services []serviceloader.Service
	for _, name := range []string{"a", "b", "c", "d"} {
		svc := newFake(name, nil)
		svc.runErr = errors.New(name + " failed")
		services = append(services, svc)
	}

	errCh := runAsync(t, context.Background(), newApp(time.Second, services...))

	require.Error(t, await(t, errCh))
}

func TestRun_StopsServicesInReverseOrder(t *testing.T) {
	rec := &recorder{}
	first, second, third := newFake("first", rec), newFake("second", rec), newFake("third", rec)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := runAsync(t, ctx, newApp(time.Second, first, second, third))

	cancel()

	require.NoError(t, await(t, errCh))
	assert.Equal(t, []string{"third", "second", "first"}, rec.get())
}

func TestRun_StopErrorsAreJoinedAndEveryServiceIsStopped(t *testing.T) {
	rec := &recorder{}
	errA, errB := errors.New("a stop"), errors.New("b stop")

	a := newFake("a", rec)
	a.stopErr = errA
	b := newFake("b", rec)
	b.stopErr = errB
	c := newFake("c", rec)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := runAsync(t, ctx, newApp(time.Second, a, b, c))

	cancel()

	err := await(t, errCh)
	require.ErrorIs(t, err, errA)
	require.ErrorIs(t, err, errB)
	assert.ElementsMatch(t, []string{"a", "b", "c"}, rec.get())
}

func TestRun_ShutdownTimeoutWhenServiceNeverReturns(t *testing.T) {
	stuck := newFake("stuck", nil)
	stuck.hang = true

	ctx, cancel := context.WithCancel(context.Background())
	errCh := runAsync(t, ctx, newApp(50*time.Millisecond, stuck))

	cancel()

	err := await(t, errCh)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestWithService_Accumulates(t *testing.T) {
	rec := &recorder{}
	a, b := newFake("a", rec), newFake("b", rec)

	app := serviceloader.New(
		serviceloader.WithLogger(slog.New(slog.NewTextHandler(discard{}, nil))),
		serviceloader.WithService(a),
		serviceloader.WithService(b),
	)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := runAsync(t, ctx, app)

	cancel()

	require.NoError(t, await(t, errCh))
	assert.ElementsMatch(t, []string{"a", "b"}, rec.get())
}

func TestNew_DefaultsDoNotPanic(t *testing.T) {
	svc := newFake("svc", nil)

	app := serviceloader.New(serviceloader.WithService(svc))

	ctx, cancel := context.WithCancel(context.Background())
	errCh := runAsync(t, ctx, app)

	cancel()

	require.NoError(t, await(t, errCh))
}
