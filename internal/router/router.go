package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func New() *chi.Mux {
	r := chi.NewRouter()

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return r
}
