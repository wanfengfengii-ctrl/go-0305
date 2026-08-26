// Command server is the runnable entry point for the hospital isolation-bearing
// construction quality-management web service. It opens the embedded relational
// store, wires the application service to the HTTP API and serves the built
// frontend console.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"hospital-isolation-bearing-unlock-closure/internal/app"
	"hospital-isolation-bearing-unlock-closure/internal/httpapi"
	"hospital-isolation-bearing-unlock-closure/internal/store"
)

func main() {
	addr := envOr("LISTEN_ADDR", ":8080")
	staticDir := envOr("STATIC_DIR", "frontend/dist")
	dbPath := envOr("DB_PATH", "benzhi.db")

	st, err := store.Open(context.Background(), dbPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	svc := app.New(st)
	defer svc.Close()

	srv := &http.Server{Addr: addr, Handler: httpapi.New(svc, staticDir).Handler()}

	go func() {
		log.Printf("listening on %s (db=%s)", addr, dbPath)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	// Graceful shutdown preserves the database on restart recovery.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Printf("shutting down")
	_ = srv.Shutdown(context.Background())
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
