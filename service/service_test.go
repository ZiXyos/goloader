package serviceloader_test

import (
	"context"
	"log/slog"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	serviceloader "github.com/zixyos/goloader/service"
	"github.com/zixyos/goloader/service/mocks"
)

// fixedID returns a Generator that always produces the same UUID,
// making injection assertions deterministic.
func fixedID(id serviceloader.UUID) serviceloader.Generator {
	return func() serviceloader.UUID { return id }
}

func TestApplicationInjectsIDIntoService(t *testing.T) {
	knownID := serviceloader.UUID([]byte("test-service-001"))

	tests := []struct {
		name        string
		appName     string
		appVersion  string
		serviceName string
		generator   serviceloader.Generator
		wantID      serviceloader.UUID
	}{
		{
			name:        "injects the generator UUID into the registered service",
			appName:     "foo",
			appVersion:  "v0.0.1",
			serviceName: "test-service",
			generator:   fixedID(knownID),
			wantID:      knownID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedID serviceloader.UUID

			started := make(chan struct{})
			done := make(chan struct{})

			svc := mocks.NewMockService(t)
			svc.EXPECT().
				SetServiceID(mock.AnythingOfType("serviceloader.UUID")).
				Run(func(id serviceloader.UUID) { capturedID = id })
			svc.EXPECT().
				Run(mock.Anything).
				RunAndReturn(func(_ context.Context) error {
					close(started)
					return nil
				})
			svc.EXPECT().
				Stop(mock.Anything).
				RunAndReturn(func(_ context.Context) error {
					close(done)
					return nil
				})

			app := serviceloader.New(
				serviceloader.WithIdentifier(tt.appName, tt.appVersion),
				serviceloader.WithGenerator(tt.generator),
				serviceloader.WithService(svc),
				serviceloader.WithLogger(slog.Default()),
			)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go app.Run(ctx)

			<-started

			p, _ := os.FindProcess(os.Getpid())
			_ = p.Signal(syscall.SIGTERM)

			<-done

			assert.Equal(t, tt.wantID, capturedID, "service should receive exactly the ID produced by its generator")
		})
	}
}
