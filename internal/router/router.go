package router

import (
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/go-playground/validator/v10"
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
		data := &Body{}
		if err := render.Bind(r, data); err != nil {
			log.Println(err)
			return
		}

		if err := rdb.Set(r.Context(), "name", data.Name, 0).Err(); err != nil {
			log.Println(err)
			return
		}

		w.WriteHeader(http.StatusCreated)
	})

	return r
}

type Body struct {
	Name string `json:"name" validate:"required"`
}

func (b *Body) Bind(r *http.Request) error {
	validate := validator.New(validator.WithRequiredStructEnabled())

	if err := validate.Struct(b); err != nil {
		errs := errors.New("Validation failed")
		for _, err := range err.(validator.ValidationErrors) {
			errs = errors.Join(errs, fmt.Errorf(
				"Field '%s': %s (value: '%v')",
				err.Field(),
				err.Tag(),
				err.Value(),
			))
		}
		return errs
	}

	return nil
}
