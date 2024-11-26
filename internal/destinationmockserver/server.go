package destinationmockserver

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
)

type DestinationMockServerConfig struct {
	Port int
}

type DestinationMockServer struct {
	logger *zap.Logger
	server *http.Server
}

func (s *DestinationMockServer) Run(ctx context.Context) error {
	go func() {
		// service connections
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Fatal("listen: %s\n", zap.Error(err))
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server with
	// a timeout of 5 seconds.
	quit := make(chan os.Signal, 1)
	// kill (no param) default send syscall.SIGTERM
	// kill -2 is syscall.SIGINT
	// kill -9 is syscall. SIGKILL but can"t be catch, so don't need add it
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	s.logger.Info("Shutdown Server ...")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.server.Shutdown(ctx); err != nil {
		s.logger.Fatal("Server Shutdown:", zap.Error(err))
	}
	// catching ctx.Done(). timeout of 5 seconds.
	select {
	case <-ctx.Done():
		s.logger.Info("timeout of 2 seconds.")
	}
	s.logger.Info("Server exiting")

	return nil
}

func New(config DestinationMockServerConfig) DestinationMockServer {
	logger, _ := zap.NewDevelopment()
	entityStore := NewEntityStore()
	router := NewRouter(entityStore)

	return DestinationMockServer{
		logger: logger,
		server: &http.Server{
			Addr:    fmt.Sprintf(":%d", config.Port),
			Handler: router,
		},
	}
}
