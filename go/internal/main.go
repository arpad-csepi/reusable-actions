package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	customSvc "github.com/arpad-csepi/go/internal/service"
)

var (
	logger        = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	listenAddress = net.JoinHostPort("127.0.0.1", "3333")
)

func main() {
	logger.Info("main started")

	server := &http.Server{Addr: listenAddress, Handler: mainService()}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("start http server", "listen_address", listenAddress)

		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error(err.Error())
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error(err.Error())
	}
}

func mainService() http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Timeout(5 * time.Second))
	r.Use(middleware.NoCache)
	// r.Use(middleware.Recoverer) // NOTE: The panic needs for be able to crash the server for testing

	r.Use(middleware.Heartbeat("/healthz"))

	serviceHello := customSvc.New("hello", 5*time.Second)
	serviceWorld := customSvc.New("world", 7*time.Second)

	r.Mount(fmt.Sprintf("/%s", serviceHello.Name), serviceHello.Router)
	r.Mount(fmt.Sprintf("/%s", serviceWorld.Name), serviceWorld.Router)

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		rootEndpointHello := fmt.Sprintf("/%s", serviceHello.Name)
		rootEndpointWorld := fmt.Sprintf("/%s", serviceWorld.Name)

		responseDataHello, statusCodeHello, err := getDataFromEndpoint(rootEndpointHello)
		if err != nil {
			http.Error(w, fmt.Sprintf("unable to check endpoint: %s\n error: %s", rootEndpointHello, err), http.StatusFailedDependency)
		}

		responseDataWorld, statusCodeWorld, err := getDataFromEndpoint(rootEndpointWorld)
		if err != nil {
			http.Error(w, fmt.Sprintf("unable to check endpoint: %s\n error: %s", rootEndpointWorld, err), http.StatusFailedDependency)
		}

		if statusCodeHello != http.StatusOK || statusCodeWorld != http.StatusOK {
			w.WriteHeader(http.StatusFailedDependency)

			return
		}

		fmt.Fprintf(w, "%s %s! :)", responseDataHello, responseDataWorld)
	})

	r.Get("/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("panic endpoint was called")
	})

	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if !serviceHello.IsTimeLimitCleared() {
			http.Error(w, fmt.Sprintf("%s service does not ready", serviceHello.Name), http.StatusServiceUnavailable)
		}

		if !serviceWorld.IsTimeLimitCleared() {
			http.Error(w, fmt.Sprintf("%s service does not ready", serviceWorld.Name), http.StatusServiceUnavailable)
		}
	})

	r.Get("/startupz", func(w http.ResponseWriter, r *http.Request) {
		healthzEndpointHello := fmt.Sprintf("/%s/healthz", serviceHello.Name)
		_, statusCodeHello, err := getDataFromEndpoint(healthzEndpointHello)
		if err != nil {
			http.Error(w, fmt.Sprintf("unable to check endpoint: %s\n error: %s", healthzEndpointHello, err), http.StatusInternalServerError)
		}

		healthzEndpointWorld := fmt.Sprintf("/%s/healthz", serviceWorld.Name)
		_, statusCodeWorld, err := getDataFromEndpoint(healthzEndpointWorld)
		if err != nil {
			http.Error(w, fmt.Sprintf("unable to check endpoint: %s\n error: %s", healthzEndpointWorld, err), http.StatusInternalServerError)
		}

		if statusCodeHello != http.StatusOK || statusCodeWorld != http.StatusOK {
			w.WriteHeader(http.StatusFailedDependency)

			return
		}
	})

	r.Get("/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(15 * time.Second)
	})

	return r
}

func getDataFromEndpoint(endpoint string) (string, int, error) {
	client := http.Client{
		Timeout: 500 * time.Millisecond,
	}

	endpointURL, err := url.Parse(fmt.Sprintf("http://%s%s", listenAddress, endpoint))
	if err != nil {
		logger.Debug("wrong endpoint is called in readyz", "host", listenAddress, "endpoint", endpoint)
	}

	response, err := client.Get(endpointURL.String())
	if err != nil {
		return "", http.StatusTeapot, err
	}

	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", http.StatusTooEarly, err
	}

	return string(body), response.StatusCode, nil
}
