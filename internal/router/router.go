package router

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/hookdeck/EventKit/internal/config"
	"github.com/redis/go-redis/v9"
)

func New() *chi.Mux {
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", config.RedisHost, config.RedisPort),
		Password: config.RedisPassword,
		DB:       config.RedisDatabase,
	})

	r := chi.NewRouter()

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r.Get("/hello", func(w http.ResponseWriter, r *http.Request) {
		name, err := rdb.Get(r.Context(), "name").Result()
		if err != nil {
			log.Println(err)
			name = "world"
		}
		w.Write([]byte(fmt.Sprintf("Hello %s!", name)))
	})

	r.Post("/hello", func(w http.ResponseWriter, r *http.Request) {
		type Body = struct {
			Name string `json:"name"`
		}

		body := &Body{}
		if err := json.NewDecoder(r.Body).Decode(body); err != nil {
			log.Println(err)
			return
		}

		if err := rdb.Set(r.Context(), "name", body.Name, 0).Err(); err != nil {
			log.Println(err)
			return
		}

		w.WriteHeader(http.StatusCreated)
	})

	return r
}
