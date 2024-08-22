package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/hookdeck/EventKit/internal/config"
)

type APIService struct {
	server *http.Server
}

func NewService(ctx context.Context, wg *sync.WaitGroup) *APIService {
	service := &APIService{}
	service.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", config.Port),
		Handler: NewRouter(),
	}

	go func() {
		defer wg.Done()
		<-ctx.Done()
		log.Println("shutting down api service")
		// make a new context for Shutdown
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := service.server.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "error shutting down http server: %s\n", err)
		}
		log.Println("http server shutted down")
	}()

	return service
}

func (s *APIService) Run(ctx context.Context) error {
	log.Println("running api service")
	log.Printf("listening on %s\n", s.server.Addr)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "error listening and serving: %s\n", err)
			// return err
		}
	}()
	return nil
}
