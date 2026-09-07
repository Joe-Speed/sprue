// sprue platform: a multi-user scale model bench with competitions.
// This file is the thin shell; everything real lives in store, images, web.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Joe-Speed/sprue/platform/store"
	"github.com/Joe-Speed/sprue/platform/web"
)

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func main() {
	dataDir := envOr("SPRUE_DATA", "data")
	port := envOr("PORT", "8080")
	baseURL := envOr("SPRUE_URL", "http://localhost:"+port)
	smtpHost := os.Getenv("SPRUE_SMTP_HOST")
	if smtpHost == "" && !strings.HasPrefix(baseURL, "http://localhost") {
		log.Fatal("sprue: SPRUE_SMTP_HOST is required unless SPRUE_URL is a localhost address; without it sign-in links would only be logged")
	}

	for _, sub := range []string{"photos", "stl"} {
		if err := os.MkdirAll(filepath.Join(dataDir, sub), 0o755); err != nil {
			log.Fatalf("sprue: cannot create %s directory: %v", sub, err)
		}
	}

	st, err := store.Open(filepath.Join(dataDir, "sprue.db"))
	if err != nil {
		log.Fatalf("sprue: %v", err)
	}
	defer st.Close()

	server, err := web.New(st, web.Config{
		DataDir:    dataDir,
		BaseURL:    baseURL,
		AdminEmail: os.Getenv("SPRUE_ADMIN_EMAIL"),
		SMTPHost:   smtpHost,
		SMTPPort:   envOr("SPRUE_SMTP_PORT", "587"),
		SMTPUser:   os.Getenv("SPRUE_SMTP_USER"),
		SMTPPass:   os.Getenv("SPRUE_SMTP_PASS"),
		SMTPFrom:   os.Getenv("SPRUE_SMTP_FROM"),
	})
	if err != nil {
		log.Fatalf("sprue: %v", err)
	}

	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    32 * 1024,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go housekeeping(ctx, st)

	go func() {
		log.Printf("sprue: serving on port %s, data in %s", port, dataDir)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("sprue: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("sprue: shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("sprue: shutdown: %v", err)
	}
}

const (
	sweepInterval = time.Hour
	shutdownGrace = 15 * time.Second
)

// housekeeping clears expired sessions and tokens and advances competitions
// by date, once at start and then every sweepInterval until the context ends.
func housekeeping(ctx context.Context, st *store.Store) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		if err := st.Sweep(); err != nil {
			log.Printf("sprue: %v", err)
		}
		if err := st.Advance(time.Now()); err != nil {
			log.Printf("sprue: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
