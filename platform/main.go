// sprue platform: a multi-user scale model bench with competitions.
// This file is the thin shell; everything real lives in store, images, web.
package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
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
		SMTPHost:   os.Getenv("SPRUE_SMTP_HOST"),
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
	log.Printf("sprue: serving on port %s, data in %s", port, dataDir)
	if err := httpServer.ListenAndServe(); err != nil {
		log.Fatalf("sprue: %v", err)
	}
}
