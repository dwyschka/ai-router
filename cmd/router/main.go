// Command router startet den project-router: HTTP-API, WebSocket-Terminal und die
// eingebettete WebUI in einem Prozess.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"project-router/internal/api"
	"project-router/internal/auth"
	"project-router/internal/config"
	"project-router/internal/notify"
	"project-router/internal/projects"
	"project-router/internal/runtime"
	"project-router/internal/session"
	"project-router/internal/store"
	webui "project-router/web"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "project-router:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "Pfad zur Konfigurationsdatei (YAML)")
	flag.Parse()

	cfg, err := config.Load(*configPath, os.Getenv)
	if err != nil {
		return err
	}
	// Validierung vor allem anderen: schlägt sie fehl, wird kein Port geöffnet.
	if err := cfg.Validate(); err != nil {
		return err
	}

	st, err := store.Open(cfg.StateDir)
	if err != nil {
		return err
	}
	catalog, err := runtime.NewCatalog(cfg.Runtimes)
	if err != nil {
		return err
	}
	registry := projects.New(st, cfg.Roots)
	sessions := session.NewManager(st, registry, catalog, cfg.BufferBytes)
	// Erkennung von Rückfragen: wartet ein Agent auf eine Entscheidung, meldet der
	// Router das an die angehängten Browser und an den konfigurierten Webhook.
	if cfg.Notify.Enabled {
		notifier, err := notify.New(cfg.Name, cfg.BaseURL, cfg.Notify)
		if err != nil {
			return err
		}
		sessions.SetWatch(notifier.Watch)
	}
	authenticator := auth.New(cfg.AuthToken(), cfg.BaseURL)

	server := api.NewServer(api.Options{
		Config:   cfg,
		Auth:     authenticator,
		Store:    st,
		Runtimes: catalog,
		Sessions: sessions,
	})

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	r.Mount("/api", server.Routes())
	// Die WebUI liegt hinter keinem Token: erst der API-Zugriff verlangt eines.
	r.Mount("/", webui.Handler())

	ln, err := net.Listen("tcp", cfg.Addr())
	if err != nil {
		return err
	}
	httpServer := &http.Server{Handler: r}

	log.SetFlags(log.LstdFlags)
	fmt.Printf("%s läuft im Modus %s: http://%s\n", cfg.Name, cfg.Mode, ln.Addr())
	for _, warnung := range cfg.Warnings() {
		fmt.Fprintln(os.Stderr, "Achtung:", warnung)
	}
	fmt.Printf("Erlaubte Roots: %v\n", cfg.Roots)
	fmt.Printf("Zustand unter: %s\n", cfg.StateDir)
	switch {
	case !cfg.Notify.Enabled:
		fmt.Println("Rückfragen werden nicht gemeldet (notify.enabled: false)")
	case cfg.Notify.Webhook.Configured():
		fmt.Printf("Rückfragen nach %s Stille an %s\n", cfg.Notify.IdleAfter, cfg.Notify.Webhook.URL)
	default:
		fmt.Printf("Rückfragen nach %s Stille nur in der WebUI — kein notify.webhook konfiguriert\n", cfg.Notify.IdleAfter)
	}

	errc := make(chan error, 1)
	go func() {
		if err := httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errc:
		return err
	case <-stop:
		fmt.Printf("\n%s wird beendet, laufende Sessions werden terminiert …\n", cfg.Name)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
	sessions.StopAll()
	return nil
}
