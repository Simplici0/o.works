package main

import (
	"html/template"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Simplici0/o.works/internal/config"
	"github.com/Simplici0/o.works/internal/db"
	"github.com/Simplici0/o.works/internal/migrations"
	"github.com/Simplici0/o.works/internal/seed"
)

type server struct {
	templates *template.Template
}

func main() {
	cfg := config.Load()

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	if cfg.IsDev() {
		if err := migrations.Up(database, "migrations"); err != nil {
			log.Fatalf("failed to run database migrations: %v", err)
		}
	}

	seedStats, err := seed.Run(database, seed.Config{
		AdminEmail:    cfg.AdminEmail,
		AdminPassword: cfg.AdminPassword,
	})
	if err != nil {
		log.Fatalf("failed to run startup seed: %v", err)
	}
	log.Printf("seed ok (inserts=%d updates=%d)", seedStats.Inserts, seedStats.Updates)

	templates, err := template.ParseFiles(
		"web/templates/layout.html",
		"web/templates/home.html",
	)
	if err != nil {
		log.Fatalf("failed to parse templates: %v", err)
	}

	srv := &server{templates: templates}

	r := chi.NewRouter()
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
	r.Get("/", srv.handleHome)

	addr := ":" + cfg.Port
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}

func (s *server) handleHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "layout.html", nil); err != nil {
		http.Error(w, "failed to render template", http.StatusInternalServerError)
		return
	}
}
