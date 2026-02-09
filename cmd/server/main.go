package main

import (
	"html/template"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type server struct {
	templates *template.Template
}

func main() {
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

	log.Println("listening on :8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
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
