package service

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Service struct {
	Name   string
	Router *chi.Mux
	State  serviceState
}

type serviceState struct {
	StartedAt time.Time
	waitTime  time.Duration
}

func New(name string, wait time.Duration) *Service {
	s := Service{
		Name:   name,
		Router: chi.NewRouter(),
		State: serviceState{
			StartedAt: time.Now().UTC(),
			waitTime:  wait,
		},
	}

	s.Router.Use(middleware.RequestID)
	s.Router.Use(middleware.RealIP)
	s.Router.Use(middleware.Logger)
	s.Router.Use(middleware.Recoverer)
	s.Router.Use(middleware.Timeout(5 * time.Second))
	s.Router.Use(middleware.NoCache)

	s.Router.Use(middleware.Heartbeat(fmt.Sprintf("/%s/healthz", name)))

	s.Router.Get("/", s.rootEndpoint)

	return &s
}

func (s *Service) rootEndpoint(w http.ResponseWriter, r *http.Request) {
	echo, err := s.echoBack(s.Name)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)

		return
	}

	w.Write(echo)

}

func (s *Service) echoBack(message string) ([]byte, error) {
	if !s.IsTimeLimitCleared() {
		return []byte{}, errors.New("time limit not cleared")
	}

	return []byte(message), nil
}

func (s *Service) IsTimeLimitCleared() bool {
	return time.Now().UTC().Sub(s.State.StartedAt) >= s.State.waitTime
}
