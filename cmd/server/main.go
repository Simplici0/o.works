package main

import (
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Simplici0/o.works/internal/config"
	"github.com/Simplici0/o.works/internal/db"
	"github.com/Simplici0/o.works/internal/migrations"
)

type server struct {
	auth *authService
	db   *sql.DB
}

type baseViewData struct {
	ErrorMessage   string
	SuccessMessage string
}

type loginViewData struct {
	baseViewData
}

type rateConfig struct {
	MachineHourlyRate  float64
	LaborPerMinute     float64
	OverheadFixed      float64
	OverheadPercent    float64
	FailureRatePercent float64
	TaxPercent         float64
	Currency           string
}

type ratesViewData struct {
	baseViewData
	RateConfig rateConfig
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

	auth := newAuthService(database, cfg.SessionSecret)
	if err := auth.ensureAdminUser(cfg.AdminEmail, cfg.AdminPassword); err != nil {
		log.Fatalf("failed to ensure admin user: %v", err)
	}

	srv := &server{auth: auth, db: database}
	if err := srv.ensureRateConfig(); err != nil {
		log.Fatalf("failed to ensure rate config: %v", err)
	}

	r := chi.NewRouter()
	r.Use(srv.authMiddleware)
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))
	r.Get("/", srv.handleHome)
	r.Get("/login", srv.handleLoginForm)
	r.Post("/login", srv.handleLoginSubmit)
	r.Post("/logout", srv.handleLogout)
	r.Get("/admin/rates", srv.handleAdminRatesForm)
	r.Post("/admin/rates", srv.handleAdminRatesSubmit)

	addr := ":" + cfg.Port
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}

func (s *server) handleHome(w http.ResponseWriter, r *http.Request) {
	s.renderTemplate(w, "home.html", nil)
}

func (s *server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if isAuthenticated(r, s.auth) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.renderTemplate(w, "login.html", loginViewData{})
}

func (s *server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	email := r.FormValue("email")
	password := r.FormValue("password")
	valid, err := s.auth.validateCredentials(email, password)
	if err != nil {
		http.Error(w, "authentication error", http.StatusInternalServerError)
		return
	}
	if !valid {
		w.WriteHeader(http.StatusUnauthorized)
		s.renderTemplate(w, "login.html", loginViewData{baseViewData: baseViewData{ErrorMessage: "Credenciales inválidas. Intenta de nuevo."}})
		return
	}

	s.auth.setSessionCookie(w, email)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.auth.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *server) handleAdminRatesForm(w http.ResponseWriter, r *http.Request) {
	rates, err := s.getRateConfig()
	if err != nil {
		http.Error(w, "failed to load rate config", http.StatusInternalServerError)
		return
	}

	s.renderTemplate(w, "admin_rates.html", ratesViewData{RateConfig: rates})
}

func (s *server) handleAdminRatesSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	rates, validationErr := parseRateConfigForm(r)
	if validationErr != nil {
		w.WriteHeader(http.StatusBadRequest)
		s.renderTemplate(w, "admin_rates.html", ratesViewData{
			baseViewData: baseViewData{ErrorMessage: validationErr.Error()},
			RateConfig:   rates,
		})
		return
	}

	if err := s.updateRateConfig(rates); err != nil {
		http.Error(w, "failed to save rate config", http.StatusInternalServerError)
		return
	}

	s.renderTemplate(w, "admin_rates.html", ratesViewData{
		baseViewData: baseViewData{SuccessMessage: "Configuración guardada correctamente."},
		RateConfig:   rates,
	})
}

func parseRateConfigForm(r *http.Request) (rateConfig, error) {
	rates := rateConfig{Currency: "COP"}

	var err error
	if rates.MachineHourlyRate, err = parseNonNegativeFloat(r.FormValue("machine_hourly_rate"), "machine_hourly_rate"); err != nil {
		return rates, err
	}
	if rates.LaborPerMinute, err = parseNonNegativeFloat(r.FormValue("labor_per_minute"), "labor_per_minute"); err != nil {
		return rates, err
	}
	if rates.OverheadFixed, err = parseNonNegativeFloat(r.FormValue("overhead_fixed"), "overhead_fixed"); err != nil {
		return rates, err
	}
	if rates.OverheadPercent, err = parsePercent(r.FormValue("overhead_percent"), "overhead_percent"); err != nil {
		return rates, err
	}
	if rates.FailureRatePercent, err = parsePercent(r.FormValue("failure_rate_percent"), "failure_rate_percent"); err != nil {
		return rates, err
	}
	if rates.TaxPercent, err = parsePercent(r.FormValue("tax_percent"), "tax_percent"); err != nil {
		return rates, err
	}

	return rates, nil
}

func parseNonNegativeFloat(raw, field string) (float64, error) {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s debe ser numérico", field)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s debe ser mayor o igual a 0", field)
	}
	return value, nil
}

func parsePercent(raw, field string) (float64, error) {
	value, err := parseNonNegativeFloat(raw, field)
	if err != nil {
		return 0, err
	}
	if value > 100 {
		return 0, fmt.Errorf("%s debe estar entre 0 y 100", field)
	}
	return value, nil
}

func (s *server) renderTemplate(w http.ResponseWriter, page string, data any) {
	templates, err := template.ParseFiles(
		"web/templates/layout.html",
		"web/templates/"+page,
	)
	if err != nil {
		http.Error(w, "failed to parse template", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, "layout.html", data); err != nil {
		http.Error(w, "failed to render template", http.StatusInternalServerError)
		return
	}
}

func (s *server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" || r.URL.Path == "/static" || strings.HasPrefix(r.URL.Path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}

		if !isAuthenticated(r, s.auth) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isAuthenticated(r *http.Request, auth *authService) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}

	_, ok := auth.verifySessionValue(cookie.Value)
	return ok
}

func (s *server) ensureRateConfig() error {
	_, err := s.db.Exec(`
		INSERT INTO rate_config (
			id,
			machine_hourly_rate,
			labor_per_minute,
			overhead_fixed,
			overhead_percent,
			failure_rate_percent,
			tax_percent,
			currency
		) VALUES (1, 0, 0, 0, 0, 0, 0, 'COP')
		ON CONFLICT(id) DO NOTHING
	`)
	if err != nil {
		return fmt.Errorf("insert default rate_config: %w", err)
	}
	return nil
}

func (s *server) getRateConfig() (rateConfig, error) {
	if err := s.ensureRateConfig(); err != nil {
		return rateConfig{}, err
	}

	var rc rateConfig
	err := s.db.QueryRow(`
		SELECT machine_hourly_rate, labor_per_minute, overhead_fixed, overhead_percent, failure_rate_percent, tax_percent, currency
		FROM rate_config
		WHERE id = 1
	`).Scan(
		&rc.MachineHourlyRate,
		&rc.LaborPerMinute,
		&rc.OverheadFixed,
		&rc.OverheadPercent,
		&rc.FailureRatePercent,
		&rc.TaxPercent,
		&rc.Currency,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return rateConfig{}, fmt.Errorf("rate_config singleton not found")
		}
		return rateConfig{}, fmt.Errorf("query rate_config: %w", err)
	}
	return rc, nil
}

func (s *server) updateRateConfig(rc rateConfig) error {
	_, err := s.db.Exec(`
		UPDATE rate_config
		SET
			machine_hourly_rate = ?,
			labor_per_minute = ?,
			overhead_fixed = ?,
			overhead_percent = ?,
			failure_rate_percent = ?,
			tax_percent = ?,
			currency = 'COP',
			updated_at = CURRENT_TIMESTAMP
		WHERE id = 1
	`,
		rc.MachineHourlyRate,
		rc.LaborPerMinute,
		rc.OverheadFixed,
		rc.OverheadPercent,
		rc.FailureRatePercent,
		rc.TaxPercent,
	)
	if err != nil {
		return fmt.Errorf("update rate_config: %w", err)
	}

	return nil
}
