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

type quoteFormValues struct {
	MaterialID    int64
	ShippingID    int64
	PackagingID   int64
	Grams         float64
	PrintMinutes  float64
	LaborMinutes  float64
	Quantity      float64
	WastePercent  float64
	MarginPercent float64
	TaxEnabled    bool
	TaxPercent    float64
}

type quoteBreakdownViewData struct {
	ErrorMessage string
	Currency     string
	Result       pricing.Result
}

type quoteViewData struct {
	Materials      []material
	ShippingRates  []shippingRate
	PackagingRates []packagingRate
	Form           quoteFormValues
	Breakdown      quoteBreakdownViewData
}

type quoteListItem struct {
	ID        int64
	CreatedAt string
	Title     string
	Total     float64
}

type quoteDetailItem struct {
	MaterialName string
	Grams        float64
	PrintMinutes float64
	LaborMinutes float64
	Quantity     float64
}

type quoteDetailViewData struct {
	ID            int64
	CreatedAt     string
	Title         string
	Notes         string
	Currency      string
	WastePercent  float64
	MarginPercent float64
	TaxEnabled    bool
	TaxPercent    float64
	Item          quoteDetailItem
	Breakdown     pricing.Breakdown
	Totals        pricing.Totals
}

type quotesViewData struct {
	baseViewData
	Query  string
	Quotes []quoteListItem
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
	r.Get("/quote", srv.handleQuoteForm)
	r.Post("/quote/calc", srv.handleQuoteCalc)
	r.Get("/quotes", srv.handleQuotesList)
	r.Get("/quotes/{id}", srv.handleQuoteDetail)
	r.Get("/quotes/{id}/text", srv.handleQuoteText)

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

func (s *server) handleQuoteForm(w http.ResponseWriter, r *http.Request) {
	materials, err := s.listActiveMaterials()
	if err != nil {
		http.Error(w, "failed to load materials", http.StatusInternalServerError)
		return
	}
	shippingRates, err := s.listActiveShippingRates()
	if err != nil {
		http.Error(w, "failed to load shipping rates", http.StatusInternalServerError)
		return
	}
	packagingRates, err := s.listActivePackagingRates()
	if err != nil {
		http.Error(w, "failed to load packaging rates", http.StatusInternalServerError)
		return
	}

	values := quoteFormValues{Quantity: 1}
	if len(materials) > 0 {
		values.MaterialID = materials[0].ID
	}

	s.renderTemplate(w, "quote.html", quoteViewData{
		Materials:      materials,
		ShippingRates:  shippingRates,
		PackagingRates: packagingRates,
		Form:           values,
		Breakdown: quoteBreakdownViewData{
			ErrorMessage: "Completa los campos para calcular.",
			Currency:     "COP",
		},
	})
}

func (s *server) handleQuoteCalc(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderBreakdownPartial(w, quoteBreakdownViewData{ErrorMessage: "Formulario inválido."})
		return
	}

	values, err := parseQuoteFormValues(r)
	if err != nil {
		s.renderBreakdownPartial(w, quoteBreakdownViewData{ErrorMessage: err.Error()})
		return
	}

	rates, err := s.getRateConfig()
	if err != nil {
		s.renderBreakdownPartial(w, quoteBreakdownViewData{ErrorMessage: "No se pudo cargar la configuración de tarifas."})
		return
	}

	selectedMaterial, err := s.getActiveMaterialByID(values.MaterialID)
	if err != nil {
		s.renderBreakdownPartial(w, quoteBreakdownViewData{ErrorMessage: err.Error()})
		return
	}

	shippingCost, err := s.getOptionalActiveShippingCost(values.ShippingID)
	if err != nil {
		s.renderBreakdownPartial(w, quoteBreakdownViewData{ErrorMessage: err.Error()})
		return
	}

	packagingCost, err := s.getOptionalActivePackagingCost(values.PackagingID)
	if err != nil {
		s.renderBreakdownPartial(w, quoteBreakdownViewData{ErrorMessage: err.Error()})
		return
	}

	result := pricing.Calculate(pricing.ItemInput{
		Grams:        values.Grams,
		PrintMinutes: values.PrintMinutes,
		LaborMinutes: values.LaborMinutes,
		Quantity:     values.Quantity,
		CostPerKg:    selectedMaterial.CostPerKg,
	}, pricing.GlobalInput{
		MachineHourlyRate:  rates.MachineHourlyRate,
		LaborPerMinute:     rates.LaborPerMinute,
		OverheadFixed:      rates.OverheadFixed,
		OverheadPercent:    rates.OverheadPercent,
		FailureRatePercent: rates.FailureRatePercent,
		WastePercent:       values.WastePercent,
		MarginPercent:      values.MarginPercent,
		TaxEnabled:         values.TaxEnabled,
		TaxPercent:         values.TaxPercent,
		PackagingCost:      packagingCost,
		ShippingCost:       shippingCost,
	})

	s.renderBreakdownPartial(w, quoteBreakdownViewData{
		Currency: rates.Currency,
		Result:   result,
	})
}

func (s *server) handleQuotesList(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	quotes, err := s.listQuotes(query)
	if err != nil {
		http.Error(w, "failed to load quotes", http.StatusInternalServerError)
		return
	}

	s.renderTemplate(w, "quotes.html", quotesViewData{
		Query:  query,
		Quotes: quotes,
	})
}

func (s *server) handleQuoteDetail(w http.ResponseWriter, r *http.Request) {
	quoteID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || quoteID <= 0 {
		http.Error(w, "invalid quote id", http.StatusBadRequest)
		return
	}

	detail, err := s.getQuoteDetail(quoteID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to load quote", http.StatusInternalServerError)
		return
	}

	s.renderTemplate(w, "quote_detail.html", detail)
}

func (s *server) handleQuoteText(w http.ResponseWriter, r *http.Request) {
	quoteID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || quoteID <= 0 {
		http.Error(w, "invalid quote id", http.StatusBadRequest)
		return
	}

	detail, err := s.getQuoteDetail(quoteID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to load quote", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(buildQuoteText(detail)))
}

func (s *server) listQuotes(query string) ([]quoteListItem, error) {
	search := "%" + query + "%"
	rows, err := s.db.Query(`
		SELECT
			id,
			created_at,
			COALESCE(title, ''),
			totals_json
		FROM quotes
		WHERE (? = '' OR COALESCE(title, '') LIKE ? OR COALESCE(notes, '') LIKE ?)
		ORDER BY datetime(created_at) DESC, id DESC
	`, query, search, search)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	quotes := make([]quoteListItem, 0)
	for rows.Next() {
		var item quoteListItem
		var totalsJSON string
		if err := rows.Scan(&item.ID, &item.CreatedAt, &item.Title, &totalsJSON); err != nil {
			return nil, err
		}
		item.Total = extractTotalFromJSON(totalsJSON)
		quotes = append(quotes, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return quotes, nil
}

func (s *server) getQuoteDetail(quoteID int64) (quoteDetailViewData, error) {
	var (
		detail        quoteDetailViewData
		breakdownJSON string
		totalsJSON    string
	)

	err := s.db.QueryRow(`
		SELECT
			q.id,
			q.created_at,
			COALESCE(q.title, ''),
			COALESCE(q.notes, ''),
			q.waste_percent,
			q.margin_percent,
			q.tax_enabled,
			q.tax_percent_snapshot,
			q.breakdown_json,
			q.totals_json,
			COALESCE(m.name, ''),
			COALESCE(qi.grams, 0),
			COALESCE(qi.print_minutes, 0),
			COALESCE(qi.labor_minutes, 0),
			COALESCE(qi.quantity, 0)
		FROM quotes q
		LEFT JOIN quote_items qi ON qi.quote_id = q.id
		LEFT JOIN materials m ON m.id = qi.material_id
		WHERE q.id = ?
		ORDER BY qi.id ASC
		LIMIT 1
	`, quoteID).Scan(
		&detail.ID,
		&detail.CreatedAt,
		&detail.Title,
		&detail.Notes,
		&detail.WastePercent,
		&detail.MarginPercent,
		&detail.TaxEnabled,
		&detail.TaxPercent,
		&breakdownJSON,
		&totalsJSON,
		&detail.Item.MaterialName,
		&detail.Item.Grams,
		&detail.Item.PrintMinutes,
		&detail.Item.LaborMinutes,
		&detail.Item.Quantity,
	)
	if err != nil {
		return quoteDetailViewData{}, err
	}

	detail.Currency = "COP"
	detail.Breakdown = extractBreakdownFromJSON(breakdownJSON)
	detail.Totals = pricing.Totals{Total: extractTotalFromJSON(totalsJSON)}

	return detail, nil
}

func extractBreakdownFromJSON(raw string) pricing.Breakdown {
	values := parseJSONNumberMap(raw)
	return pricing.Breakdown{
		MaterialCost:     pickFloat(values, "material_cost", "material"),
		MachineCost:      pickFloat(values, "machine_cost", "machine"),
		LaborCost:        pickFloat(values, "labor_cost", "labor"),
		Subtotal:         pickFloat(values, "subtotal"),
		Overhead:         pickFloat(values, "overhead"),
		FailureInsurance: pickFloat(values, "failure_insurance", "failure"),
		PackagingCost:    pickFloat(values, "packaging_cost", "packaging"),
		ShippingCost:     pickFloat(values, "shipping_cost", "shipping"),
		Margin:           pickFloat(values, "margin"),
		Tax:              pickFloat(values, "tax"),
	}
}

func parseJSONNumberMap(raw string) map[string]float64 {
	values := map[string]float64{}
	decoder := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &decoder); err != nil {
		return values
	}
	collectNumericValues(values, "", decoder)
	return values
}

func collectNumericValues(values map[string]float64, prefix string, source map[string]any) {
	for key, val := range source {
		normalizedKey := normalizeJSONKey(key)
		if prefix != "" {
			normalizedKey = prefix + "_" + normalizedKey
		}

		switch typedVal := val.(type) {
		case float64:
			values[normalizedKey] = typedVal
		case map[string]any:
			collectNumericValues(values, normalizedKey, typedVal)
		}
	}
}

func normalizeJSONKey(key string) string {
	key = strings.TrimSpace(strings.ToLower(key))
	key = strings.ReplaceAll(key, "-", "_")
	return key
}

func pickFloat(values map[string]float64, keys ...string) float64 {
	for _, key := range keys {
		if value, ok := values[normalizeJSONKey(key)]; ok {
			return value
		}
		if value, ok := values["breakdown_"+normalizeJSONKey(key)]; ok {
			return value
		}
	}
	return 0
}

func buildQuoteText(detail quoteDetailViewData) string {
	title := detail.Title
	if title == "" {
		title = fmt.Sprintf("Cotización #%d", detail.ID)
	}

	lines := []string{
		title,
		fmt.Sprintf("Fecha: %s", detail.CreatedAt),
		"",
		fmt.Sprintf("Total: %.2f %s", detail.Totals.Total, detail.Currency),
		"",
		"Desglose resumido:",
		fmt.Sprintf("- Material: %.2f %s", detail.Breakdown.MaterialCost, detail.Currency),
		fmt.Sprintf("- Máquina: %.2f %s", detail.Breakdown.MachineCost, detail.Currency),
		fmt.Sprintf("- Mano de obra: %.2f %s", detail.Breakdown.LaborCost, detail.Currency),
		fmt.Sprintf("- Subtotal: %.2f %s", detail.Breakdown.Subtotal, detail.Currency),
		fmt.Sprintf("- Overhead: %.2f %s", detail.Breakdown.Overhead, detail.Currency),
		fmt.Sprintf("- Seguro de falla: %.2f %s", detail.Breakdown.FailureInsurance, detail.Currency),
		fmt.Sprintf("- Packaging: %.2f %s", detail.Breakdown.PackagingCost, detail.Currency),
		fmt.Sprintf("- Shipping: %.2f %s", detail.Breakdown.ShippingCost, detail.Currency),
		fmt.Sprintf("- Margen: %.2f %s", detail.Breakdown.Margin, detail.Currency),
		fmt.Sprintf("- Impuesto: %.2f %s", detail.Breakdown.Tax, detail.Currency),
		"",
		"Supuestos:",
		fmt.Sprintf("- Merma: %.2f%%", detail.WastePercent),
		fmt.Sprintf("- Margen: %.2f%%", detail.MarginPercent),
		fmt.Sprintf("- Impuesto: %s (%.2f%%)", map[bool]string{true: "incluido", false: "no incluido"}[detail.TaxEnabled], detail.TaxPercent),
		"",
		"Datos del item:",
		fmt.Sprintf("- Material: %s", detail.Item.MaterialName),
		fmt.Sprintf("- Gramos: %.2f", detail.Item.Grams),
		fmt.Sprintf("- Tiempo impresión: %.2f min", detail.Item.PrintMinutes),
		fmt.Sprintf("- Mano de obra: %.2f min", detail.Item.LaborMinutes),
		fmt.Sprintf("- Cantidad: %.0f", detail.Item.Quantity),
	}

	if detail.Notes != "" {
		lines = append(lines, "", "Notas:", detail.Notes)
	}

	return strings.Join(lines, "\n")
}

func extractTotalFromJSON(totalsJSON string) float64 {
	var values map[string]float64
	if err := json.Unmarshal([]byte(totalsJSON), &values); err != nil {
		return 0
	}

	for _, key := range []string{"total", "grand_total", "final_total"} {
		if total, ok := values[key]; ok {
			return total
		}
	}

	return 0
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

func parseQuoteFormValues(r *http.Request) (quoteFormValues, error) {
	values := quoteFormValues{}

	var err error
	if values.MaterialID, err = parseRequiredID(r.FormValue("material_id"), "material_id"); err != nil {
		return values, err
	}
	if values.ShippingID, err = parseOptionalID(r.FormValue("shipping_id")); err != nil {
		return values, fmt.Errorf("shipping_id inválido")
	}
	if values.PackagingID, err = parseOptionalID(r.FormValue("packaging_id")); err != nil {
		return values, fmt.Errorf("packaging_id inválido")
	}
	if values.Grams, err = parsePositiveFloat(r.FormValue("grams"), "grams"); err != nil {
		return values, err
	}
	if values.PrintMinutes, err = parseNonNegativeFloat(r.FormValue("printMinutes"), "printMinutes"); err != nil {
		return values, err
	}
	if values.LaborMinutes, err = parseNonNegativeFloat(r.FormValue("laborMinutes"), "laborMinutes"); err != nil {
		return values, err
	}
	if values.Quantity, err = parsePositiveFloat(r.FormValue("quantity"), "quantity"); err != nil {
		return values, err
	}
	if values.WastePercent, err = parsePercent(r.FormValue("wastePercent"), "wastePercent"); err != nil {
		return values, err
	}
	if values.MarginPercent, err = parsePercent(r.FormValue("marginPercent"), "marginPercent"); err != nil {
		return values, err
	}

	values.TaxEnabled = r.FormValue("taxEnabled") == "1"
	if values.TaxPercent, err = parsePercent(r.FormValue("taxPercent"), "taxPercent"); err != nil {
		return values, err
	}

	return values, nil
}

func parseRequiredID(raw, field string) (int64, error) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s inválido", field)
	}
	return value, nil
}

func parseOptionalID(raw string) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("id inválido")
	}
	return value, nil
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

func (s *server) renderBreakdownPartial(w http.ResponseWriter, data quoteBreakdownViewData) {
	tmpl, err := template.ParseFiles("web/templates/quote_breakdown_partial.html")
	if err != nil {
		http.Error(w, "failed to parse template", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
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

func (s *server) listActiveMaterials() ([]material, error) {
	rows, err := s.db.Query(`
		SELECT id, name, cost_per_kg, COALESCE(notes, ''), active
		FROM materials
		WHERE active = TRUE
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query active materials: %w", err)
	}
	defer rows.Close()

	materials := make([]material, 0)
	for rows.Next() {
		var m material
		if err := rows.Scan(&m.ID, &m.Name, &m.CostPerKg, &m.Notes, &m.Active); err != nil {
			return nil, fmt.Errorf("scan active material: %w", err)
		}
		materials = append(materials, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active materials: %w", err)
	}

	return materials, nil
}

func (s *server) getActiveMaterialByID(id int64) (material, error) {
	var m material
	err := s.db.QueryRow(`
		SELECT id, name, cost_per_kg, COALESCE(notes, ''), active
		FROM materials
		WHERE id = ? AND active = TRUE
	`, id).Scan(&m.ID, &m.Name, &m.CostPerKg, &m.Notes, &m.Active)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return material{}, fmt.Errorf("material no encontrado o inactivo")
		}
		return material{}, fmt.Errorf("query material: %w", err)
	}
	return m, nil
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

func (s *server) listActiveShippingRates() ([]shippingRate, error) {
	rows, err := s.db.Query(`
		SELECT id, scope, country, COALESCE(city, ''), flat_cost, COALESCE(notes, ''), active
		FROM shipping_rates
		WHERE active = TRUE
		ORDER BY id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query active shipping rates: %w", err)
	}
	defer rows.Close()

	shippingRates := make([]shippingRate, 0)
	for rows.Next() {
		var rate shippingRate
		if err := rows.Scan(&rate.ID, &rate.Scope, &rate.Country, &rate.City, &rate.FlatCost, &rate.Notes, &rate.Active); err != nil {
			return nil, fmt.Errorf("scan active shipping rate: %w", err)
		}
		shippingRates = append(shippingRates, rate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active shipping rates: %w", err)
	}

	return shippingRates, nil
}

func (s *server) getOptionalActiveShippingCost(id int64) (float64, error) {
	if id == 0 {
		return 0, nil
	}

	var cost float64
	err := s.db.QueryRow(`SELECT flat_cost FROM shipping_rates WHERE id = ? AND active = TRUE`, id).Scan(&cost)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("shipping no encontrado o inactivo")
		}
		return 0, fmt.Errorf("query shipping: %w", err)
	}

	return cost, nil
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

func (s *server) listActivePackagingRates() ([]packagingRate, error) {
	rows, err := s.db.Query(`
		SELECT id, name, flat_cost, COALESCE(notes, ''), active
		FROM packaging_rates
		WHERE active = TRUE
		ORDER BY id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query active packaging rates: %w", err)
	}
	defer rows.Close()

	packagingRates := make([]packagingRate, 0)
	for rows.Next() {
		var rate packagingRate
		if err := rows.Scan(&rate.ID, &rate.Name, &rate.FlatCost, &rate.Notes, &rate.Active); err != nil {
			return nil, fmt.Errorf("scan active packaging rate: %w", err)
		}
		packagingRates = append(packagingRates, rate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active packaging rates: %w", err)
	}

	return packagingRates, nil
}

func (s *server) getOptionalActivePackagingCost(id int64) (float64, error) {
	if id == 0 {
		return 0, nil
	}

	var cost float64
	err := s.db.QueryRow(`SELECT flat_cost FROM packaging_rates WHERE id = ? AND active = TRUE`, id).Scan(&cost)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("packaging no encontrado o inactivo")
		}
		return 0, fmt.Errorf("query packaging: %w", err)
	}

	return cost, nil
}
