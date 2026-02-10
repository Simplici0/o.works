package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Simplici0/o.works/internal/config"
	"github.com/Simplici0/o.works/internal/db"
	"github.com/Simplici0/o.works/internal/migrations"
	"github.com/Simplici0/o.works/internal/pricing"
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

type material struct {
	ID        int64
	Name      string
	CostPerKg float64
	Notes     string
	Active    bool
}

type materialsViewData struct {
	baseViewData
	Materials []material
}

type shippingRate struct {
	ID       int64
	Scope    string
	Country  string
	City     string
	FlatCost float64
	Notes    string
	Active   bool
}

type shippingViewData struct {
	baseViewData
	ShippingRates []shippingRate
}

type packagingRate struct {
	ID       int64
	Name     string
	FlatCost float64
	Notes    string
	Active   bool
}

type packagingViewData struct {
	baseViewData
	PackagingRates []packagingRate
}

type quoteItemInput struct {
	MaterialID   int64
	Grams        float64
	PrintMinutes float64
	LaborMinutes float64
	Quantity     int
}

type quoteCalcBreakdown struct {
	MaterialCost     float64 `json:"material_cost"`
	MachineCost      float64 `json:"machine_cost"`
	LaborCost        float64 `json:"labor_cost"`
	Subtotal         float64 `json:"subtotal"`
	Overhead         float64 `json:"overhead"`
	FailureInsurance float64 `json:"failure_insurance"`
	PackagingCost    float64 `json:"packaging_cost"`
	ShippingCost     float64 `json:"shipping_cost"`
	Margin           float64 `json:"margin"`
	Tax              float64 `json:"tax"`
}

type quoteCalcTotals struct {
	Total float64 `json:"total"`
}

type quoteSummary struct {
	ID        int64
	CreatedAt string
	Title     string
	Total     float64
}

type quotesViewData struct {
	Quotes []quoteSummary
}

type quoteDetailViewData struct {
	ID        int64
	CreatedAt string
	Title     string
	Notes     string

	WastePercent       float64
	MarginPercent      float64
	TaxEnabled         bool
	TaxPercentSnapshot float64

	TotalsJSON    string
	BreakdownJSON string
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
	r.Get("/admin/materials", srv.handleAdminMaterialsForm)
	r.Post("/admin/materials", srv.handleAdminMaterialsCreate)
	r.Post("/admin/materials/{id}", srv.handleAdminMaterialsUpdate)
	r.Get("/admin/shipping", srv.handleAdminShippingForm)
	r.Post("/admin/shipping", srv.handleAdminShippingCreate)
	r.Post("/admin/shipping/{id}", srv.handleAdminShippingUpdate)
	r.Get("/admin/packaging", srv.handleAdminPackagingForm)
	r.Post("/admin/packaging", srv.handleAdminPackagingCreate)
	r.Post("/admin/packaging/{id}", srv.handleAdminPackagingUpdate)
	r.Get("/quotes", srv.handleQuotesList)
	r.Get("/quotes/{id}", srv.handleQuoteDetail)
	r.Post("/quote/save", srv.handleQuoteSave)

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

func (s *server) handleAdminMaterialsForm(w http.ResponseWriter, r *http.Request) {
	materials, err := s.listMaterials()
	if err != nil {
		http.Error(w, "failed to load materials", http.StatusInternalServerError)
		return
	}

	s.renderTemplate(w, "admin_materials.html", materialsViewData{
		baseViewData: baseViewData{
			ErrorMessage:   r.URL.Query().Get("error"),
			SuccessMessage: r.URL.Query().Get("success"),
		},
		Materials: materials,
	})
}

func (s *server) handleAdminMaterialsCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	notes := strings.TrimSpace(r.FormValue("notes"))
	if name == "" {
		http.Redirect(w, r, "/admin/materials?error=name+es+requerido", http.StatusSeeOther)
		return
	}

	costPerKg, err := parsePositiveFloat(r.FormValue("cost_per_kg"), "cost_per_kg")
	if err != nil {
		http.Redirect(w, r, "/admin/materials?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	_, err = s.db.Exec(`
		INSERT INTO materials (name, cost_per_kg, notes, active)
		VALUES (?, ?, ?, TRUE)
	`, name, costPerKg, notes)
	if err != nil {
		http.Error(w, "failed to create material", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/materials?success=Material+creado+correctamente", http.StatusSeeOther)
}

func (s *server) handleAdminMaterialsUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid material id", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	notes := strings.TrimSpace(r.FormValue("notes"))
	if name == "" {
		http.Redirect(w, r, "/admin/materials?error=name+es+requerido", http.StatusSeeOther)
		return
	}

	costPerKg, err := parsePositiveFloat(r.FormValue("cost_per_kg"), "cost_per_kg")
	if err != nil {
		http.Redirect(w, r, "/admin/materials?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	active := r.FormValue("active") == "1"

	result, err := s.db.Exec(`
		UPDATE materials
		SET
			name = ?,
			cost_per_kg = ?,
			notes = ?,
			active = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, name, costPerKg, notes, active, id)
	if err != nil {
		http.Error(w, "failed to update material", http.StatusInternalServerError)
		return
	}

	affected, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "failed to update material", http.StatusInternalServerError)
		return
	}
	if affected == 0 {
		http.NotFound(w, r)
		return
	}

	http.Redirect(w, r, "/admin/materials?success=Material+actualizado+correctamente", http.StatusSeeOther)
}

func (s *server) handleAdminShippingForm(w http.ResponseWriter, r *http.Request) {
	shippingRates, err := s.listShippingRates()
	if err != nil {
		http.Error(w, "failed to load shipping rates", http.StatusInternalServerError)
		return
	}

	s.renderTemplate(w, "admin_shipping.html", shippingViewData{
		baseViewData: baseViewData{
			ErrorMessage:   r.URL.Query().Get("error"),
			SuccessMessage: r.URL.Query().Get("success"),
		},
		ShippingRates: shippingRates,
	})
}

func (s *server) handleAdminShippingCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	rate, err := parseShippingRateForm(r)
	if err != nil {
		http.Redirect(w, r, "/admin/shipping?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	_, err = s.db.Exec(`
		INSERT INTO shipping_rates (scope, country, city, flat_cost, notes, active)
		VALUES (?, ?, ?, ?, ?, ?)
	`, rate.Scope, rate.Country, rate.City, rate.FlatCost, rate.Notes, rate.Active)
	if err != nil {
		http.Error(w, "failed to create shipping rate", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/shipping?success=Tarifa+de+env%C3%ADo+creada+correctamente", http.StatusSeeOther)
}

func (s *server) handleAdminShippingUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid shipping rate id", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	rate, err := parseShippingRateForm(r)
	if err != nil {
		http.Redirect(w, r, "/admin/shipping?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	result, err := s.db.Exec(`
		UPDATE shipping_rates
		SET
			scope = ?,
			country = ?,
			city = ?,
			flat_cost = ?,
			notes = ?,
			active = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, rate.Scope, rate.Country, rate.City, rate.FlatCost, rate.Notes, rate.Active, id)
	if err != nil {
		http.Error(w, "failed to update shipping rate", http.StatusInternalServerError)
		return
	}

	affected, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "failed to update shipping rate", http.StatusInternalServerError)
		return
	}
	if affected == 0 {
		http.NotFound(w, r)
		return
	}

	http.Redirect(w, r, "/admin/shipping?success=Tarifa+de+env%C3%ADo+actualizada+correctamente", http.StatusSeeOther)
}

func (s *server) handleAdminPackagingForm(w http.ResponseWriter, r *http.Request) {
	packagingRates, err := s.listPackagingRates()
	if err != nil {
		http.Error(w, "failed to load packaging rates", http.StatusInternalServerError)
		return
	}

	s.renderTemplate(w, "admin_packaging.html", packagingViewData{
		baseViewData: baseViewData{
			ErrorMessage:   r.URL.Query().Get("error"),
			SuccessMessage: r.URL.Query().Get("success"),
		},
		PackagingRates: packagingRates,
	})
}

func (s *server) handleAdminPackagingCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	rate, err := parsePackagingRateForm(r)
	if err != nil {
		http.Redirect(w, r, "/admin/packaging?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	_, err = s.db.Exec(`
		INSERT INTO packaging_rates (name, flat_cost, notes, active)
		VALUES (?, ?, ?, ?)
	`, rate.Name, rate.FlatCost, rate.Notes, rate.Active)
	if err != nil {
		http.Error(w, "failed to create packaging rate", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/packaging?success=Tarifa+de+empaque+creada+correctamente", http.StatusSeeOther)
}

func (s *server) handleAdminPackagingUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid packaging rate id", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	rate, err := parsePackagingRateForm(r)
	if err != nil {
		http.Redirect(w, r, "/admin/packaging?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	result, err := s.db.Exec(`
		UPDATE packaging_rates
		SET
			name = ?,
			flat_cost = ?,
			notes = ?,
			active = ?,
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, rate.Name, rate.FlatCost, rate.Notes, rate.Active, id)
	if err != nil {
		http.Error(w, "failed to update packaging rate", http.StatusInternalServerError)
		return
	}

	affected, err := result.RowsAffected()
	if err != nil {
		http.Error(w, "failed to update packaging rate", http.StatusInternalServerError)
		return
	}
	if affected == 0 {
		http.NotFound(w, r)
		return
	}

	http.Redirect(w, r, "/admin/packaging?success=Tarifa+de+empaque+actualizada+correctamente", http.StatusSeeOther)
}

func (s *server) handleQuoteSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	rates, err := s.getRateConfig()
	if err != nil {
		http.Error(w, "failed to load rate config", http.StatusInternalServerError)
		return
	}

	items, err := parseQuoteItemsForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	wastePercent, err := parsePercent(r.FormValue("waste_percent"), "waste_percent")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	marginPercent, err := parsePercent(r.FormValue("margin_percent"), "margin_percent")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	packagingCost, err := parseNonNegativeFloat(r.FormValue("packaging_cost"), "packaging_cost")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	shippingCost, err := parseNonNegativeFloat(r.FormValue("shipping_cost"), "shipping_cost")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	taxEnabled := r.FormValue("tax_enabled") == "1" || strings.EqualFold(r.FormValue("tax_enabled"), "true") || strings.EqualFold(r.FormValue("tax_enabled"), "on")

	breakdown, totals, err := s.calculateQuoteSnapshot(items, pricing.GlobalInput{
		MachineHourlyRate:  rates.MachineHourlyRate,
		LaborPerMinute:     rates.LaborPerMinute,
		OverheadFixed:      rates.OverheadFixed,
		OverheadPercent:    rates.OverheadPercent,
		FailureRatePercent: rates.FailureRatePercent,
		WastePercent:       wastePercent,
		MarginPercent:      marginPercent,
		TaxEnabled:         taxEnabled,
		TaxPercent:         rates.TaxPercent,
		PackagingCost:      packagingCost,
		ShippingCost:       shippingCost,
	})
	if err != nil {
		http.Error(w, "failed to calculate quote", http.StatusBadRequest)
		return
	}

	totalsJSON, err := json.Marshal(totals)
	if err != nil {
		http.Error(w, "failed to serialize totals", http.StatusInternalServerError)
		return
	}

	breakdownJSON, err := json.Marshal(breakdown)
	if err != nil {
		http.Error(w, "failed to serialize breakdown", http.StatusInternalServerError)
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	notes := strings.TrimSpace(r.FormValue("notes"))

	tx, err := s.db.Begin()
	if err != nil {
		http.Error(w, "failed to start transaction", http.StatusInternalServerError)
		return
	}

	result, err := tx.Exec(`
		INSERT INTO quotes (
			title,
			notes,
			waste_percent,
			margin_percent,
			tax_enabled,
			tax_percent_snapshot,
			totals_json,
			breakdown_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, nullableString(title), nullableString(notes), wastePercent, marginPercent, taxEnabled, rates.TaxPercent, string(totalsJSON), string(breakdownJSON))
	if err != nil {
		_ = tx.Rollback()
		http.Error(w, "failed to save quote", http.StatusInternalServerError)
		return
	}

	quoteID, err := result.LastInsertId()
	if err != nil {
		_ = tx.Rollback()
		http.Error(w, "failed to save quote", http.StatusInternalServerError)
		return
	}

	for _, item := range items {
		_, err = tx.Exec(`
			INSERT INTO quote_items (quote_id, material_id, grams, print_minutes, labor_minutes, quantity)
			VALUES (?, ?, ?, ?, ?, ?)
		`, quoteID, item.MaterialID, item.Grams, item.PrintMinutes, item.LaborMinutes, item.Quantity)
		if err != nil {
			_ = tx.Rollback()
			http.Error(w, "failed to save quote items", http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "failed to save quote", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/quotes/%d", quoteID), http.StatusSeeOther)
}

func (s *server) handleQuotesList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT id, datetime(created_at), COALESCE(title, ''), totals_json
		FROM quotes
		ORDER BY id DESC
	`)
	if err != nil {
		http.Error(w, "failed to load quotes", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	quotes := make([]quoteSummary, 0)
	for rows.Next() {
		var q quoteSummary
		var totalsJSON string
		if err := rows.Scan(&q.ID, &q.CreatedAt, &q.Title, &totalsJSON); err != nil {
			http.Error(w, "failed to load quotes", http.StatusInternalServerError)
			return
		}

		var totals quoteCalcTotals
		if err := json.Unmarshal([]byte(totalsJSON), &totals); err == nil {
			q.Total = totals.Total
		}

		quotes = append(quotes, q)
	}

	s.renderTemplate(w, "quotes.html", quotesViewData{Quotes: quotes})
}

func (s *server) handleQuoteDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid quote id", http.StatusBadRequest)
		return
	}

	var data quoteDetailViewData
	var title sql.NullString
	var notes sql.NullString
	err = s.db.QueryRow(`
		SELECT id, datetime(created_at), title, notes, waste_percent, margin_percent, tax_enabled, tax_percent_snapshot, totals_json, breakdown_json
		FROM quotes
		WHERE id = ?
	`, id).Scan(
		&data.ID,
		&data.CreatedAt,
		&title,
		&notes,
		&data.WastePercent,
		&data.MarginPercent,
		&data.TaxEnabled,
		&data.TaxPercentSnapshot,
		&data.TotalsJSON,
		&data.BreakdownJSON,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to load quote", http.StatusInternalServerError)
		return
	}

	if title.Valid {
		data.Title = title.String
	}
	if notes.Valid {
		data.Notes = notes.String
	}

	s.renderTemplate(w, "quote_detail.html", data)
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func parseQuoteItemsForm(r *http.Request) ([]quoteItemInput, error) {
	materialIDs := r.Form["material_id"]
	gramsValues := r.Form["grams"]
	printMinutesValues := r.Form["print_minutes"]
	laborMinutesValues := r.Form["labor_minutes"]
	quantityValues := r.Form["quantity"]

	count := len(materialIDs)
	if count == 0 {
		return nil, fmt.Errorf("material_id es requerido")
	}
	if len(gramsValues) != count || len(printMinutesValues) != count || len(laborMinutesValues) != count || len(quantityValues) != count {
		return nil, fmt.Errorf("items incompletos")
	}

	items := make([]quoteItemInput, 0, count)
	for i := 0; i < count; i++ {
		materialID, err := strconv.ParseInt(materialIDs[i], 10, 64)
		if err != nil || materialID <= 0 {
			return nil, fmt.Errorf("material_id inválido en item %d", i+1)
		}

		grams, err := parsePositiveFloat(gramsValues[i], "grams")
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", i+1, err)
		}

		printMinutes, err := parsePositiveFloat(printMinutesValues[i], "print_minutes")
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", i+1, err)
		}

		laborMinutes, err := parseNonNegativeFloat(laborMinutesValues[i], "labor_minutes")
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", i+1, err)
		}

		quantity, err := strconv.Atoi(quantityValues[i])
		if err != nil || quantity <= 0 {
			return nil, fmt.Errorf("item %d: quantity debe ser mayor a 0", i+1)
		}

		items = append(items, quoteItemInput{
			MaterialID:   materialID,
			Grams:        grams,
			PrintMinutes: printMinutes,
			LaborMinutes: laborMinutes,
			Quantity:     quantity,
		})
	}

	return items, nil
}

func (s *server) calculateQuoteSnapshot(items []quoteItemInput, global pricing.GlobalInput) (quoteCalcBreakdown, quoteCalcTotals, error) {
	if len(items) == 0 {
		return quoteCalcBreakdown{}, quoteCalcTotals{}, fmt.Errorf("at least one item is required")
	}

	breakdown := quoteCalcBreakdown{}
	for _, item := range items {
		var costPerKg float64
		err := s.db.QueryRow(`
			SELECT cost_per_kg
			FROM materials
			WHERE id = ? AND active = TRUE
		`, item.MaterialID).Scan(&costPerKg)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return quoteCalcBreakdown{}, quoteCalcTotals{}, fmt.Errorf("material not found")
			}
			return quoteCalcBreakdown{}, quoteCalcTotals{}, err
		}

		lineMaterialCost := (item.Grams / 1000.0) * costPerKg * (1.0 + global.WastePercent/100.0)
		lineMachineCost := (item.PrintMinutes / 60.0) * global.MachineHourlyRate
		lineLaborCost := item.LaborMinutes * global.LaborPerMinute
		quantity := float64(item.Quantity)

		breakdown.MaterialCost += lineMaterialCost * quantity
		breakdown.MachineCost += lineMachineCost * quantity
		breakdown.LaborCost += lineLaborCost * quantity
	}

	breakdown.Subtotal = breakdown.MaterialCost + breakdown.MachineCost + breakdown.LaborCost
	breakdown.Overhead = global.OverheadFixed + breakdown.Subtotal*(global.OverheadPercent/100.0)
	breakdown.FailureInsurance = breakdown.Subtotal * (global.FailureRatePercent / 100.0)
	breakdown.PackagingCost = global.PackagingCost
	breakdown.ShippingCost = global.ShippingCost
	breakdown.Margin = (global.MarginPercent / 100.0) * (breakdown.Subtotal + breakdown.Overhead + breakdown.FailureInsurance)

	if global.TaxEnabled {
		breakdown.Tax = (global.TaxPercent / 100.0) * (breakdown.Subtotal + breakdown.Overhead + breakdown.FailureInsurance + breakdown.Margin)
	}

	totals := quoteCalcTotals{
		Total: breakdown.Subtotal + breakdown.Overhead + breakdown.FailureInsurance + breakdown.PackagingCost + breakdown.ShippingCost + breakdown.Margin + breakdown.Tax,
	}

	return breakdown, totals, nil
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

func parsePositiveFloat(raw, field string) (float64, error) {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s debe ser numérico", field)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s debe ser mayor a 0", field)
	}
	return value, nil
}

func parseShippingRateForm(r *http.Request) (shippingRate, error) {
	rate := shippingRate{
		Scope:   strings.TrimSpace(r.FormValue("scope")),
		Country: strings.TrimSpace(r.FormValue("country")),
		City:    strings.TrimSpace(r.FormValue("city")),
		Notes:   strings.TrimSpace(r.FormValue("notes")),
		Active:  r.FormValue("active") == "1",
	}

	if rate.Scope != "CO" && rate.Scope != "INTL" {
		return rate, fmt.Errorf("scope debe ser CO o INTL")
	}
	if rate.Country == "" {
		return rate, fmt.Errorf("country es requerido")
	}

	var err error
	rate.FlatCost, err = parseNonNegativeFloat(r.FormValue("flat_cost"), "flat_cost")
	if err != nil {
		return rate, err
	}

	return rate, nil
}

func parsePackagingRateForm(r *http.Request) (packagingRate, error) {
	rate := packagingRate{
		Name:   strings.TrimSpace(r.FormValue("name")),
		Notes:  strings.TrimSpace(r.FormValue("notes")),
		Active: r.FormValue("active") == "1",
	}

	if rate.Name == "" {
		return rate, fmt.Errorf("name es requerido")
	}

	var err error
	rate.FlatCost, err = parseNonNegativeFloat(r.FormValue("flat_cost"), "flat_cost")
	if err != nil {
		return rate, err
	}

	return rate, nil
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

func (s *server) listMaterials() ([]material, error) {
	rows, err := s.db.Query(`
		SELECT id, name, cost_per_kg, COALESCE(notes, ''), active
		FROM materials
		ORDER BY id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query materials: %w", err)
	}
	defer rows.Close()

	materials := make([]material, 0)
	for rows.Next() {
		var m material
		if err := rows.Scan(&m.ID, &m.Name, &m.CostPerKg, &m.Notes, &m.Active); err != nil {
			return nil, fmt.Errorf("scan material: %w", err)
		}
		materials = append(materials, m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate materials: %w", err)
	}

	return materials, nil
}

func (s *server) listShippingRates() ([]shippingRate, error) {
	rows, err := s.db.Query(`
		SELECT id, scope, country, COALESCE(city, ''), flat_cost, COALESCE(notes, ''), active
		FROM shipping_rates
		ORDER BY id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query shipping rates: %w", err)
	}
	defer rows.Close()

	shippingRates := make([]shippingRate, 0)
	for rows.Next() {
		var rate shippingRate
		if err := rows.Scan(&rate.ID, &rate.Scope, &rate.Country, &rate.City, &rate.FlatCost, &rate.Notes, &rate.Active); err != nil {
			return nil, fmt.Errorf("scan shipping rate: %w", err)
		}
		shippingRates = append(shippingRates, rate)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate shipping rates: %w", err)
	}

	return shippingRates, nil
}

func (s *server) listPackagingRates() ([]packagingRate, error) {
	rows, err := s.db.Query(`
		SELECT id, name, flat_cost, COALESCE(notes, ''), active
		FROM packaging_rates
		ORDER BY id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query packaging rates: %w", err)
	}
	defer rows.Close()

	packagingRates := make([]packagingRate, 0)
	for rows.Next() {
		var rate packagingRate
		if err := rows.Scan(&rate.ID, &rate.Name, &rate.FlatCost, &rate.Notes, &rate.Active); err != nil {
			return nil, fmt.Errorf("scan packaging rate: %w", err)
		}
		packagingRates = append(packagingRates, rate)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate packaging rates: %w", err)
	}

	return packagingRates, nil
}
