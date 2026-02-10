package main

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	_ "modernc.org/sqlite"
)

func TestGetQuoteDetailReadsSnapshotWithoutRecalculation(t *testing.T) {
	db := newQuoteDetailTestDB(t)
	srv := &server{db: db}

	seedQuoteDetail(t, db)

	detail, err := srv.getQuoteDetail(1)
	if err != nil {
		t.Fatalf("getQuoteDetail returned error: %v", err)
	}

	if detail.Breakdown.MaterialCost != 123.45 {
		t.Fatalf("expected snapshot material cost 123.45, got %.2f", detail.Breakdown.MaterialCost)
	}
	if detail.Totals.Total != 999.99 {
		t.Fatalf("expected snapshot total 999.99, got %.2f", detail.Totals.Total)
	}
	if detail.Item.MaterialName != "PLA Pro" || detail.Item.Quantity != 3 {
		t.Fatalf("unexpected item detail: %+v", detail.Item)
	}
}

func TestHandleQuoteTextReturnsPlainText(t *testing.T) {
	db := newQuoteDetailTestDB(t)
	srv := &server{db: db}
	seedQuoteDetail(t, db)

	req := httptest.NewRequest(http.MethodGet, "/quotes/1/text", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	srv.handleQuoteText(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("expected text/plain content type, got %q", rr.Header().Get("Content-Type"))
	}

	body := rr.Body.String()
	for _, expected := range []string{"Total: 999.99 COP", "Supuestos:", "Datos del item:", "Material: PLA Pro"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("expected body to contain %q, got: %s", expected, body)
		}
	}
}

func newQuoteDetailTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE materials (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL
		);
		CREATE TABLE quotes (
			id INTEGER PRIMARY KEY,
			created_at DATETIME NOT NULL,
			title TEXT,
			notes TEXT,
			waste_percent NUMERIC NOT NULL,
			margin_percent NUMERIC NOT NULL,
			tax_enabled BOOLEAN NOT NULL,
			tax_percent_snapshot NUMERIC NOT NULL,
			totals_json TEXT NOT NULL,
			breakdown_json TEXT NOT NULL
		);
		CREATE TABLE quote_items (
			id INTEGER PRIMARY KEY,
			quote_id INTEGER NOT NULL,
			material_id INTEGER NOT NULL,
			grams NUMERIC NOT NULL,
			print_minutes NUMERIC NOT NULL,
			labor_minutes NUMERIC NOT NULL,
			quantity INTEGER NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("failed creating schema: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedQuoteDetail(t *testing.T, db *sql.DB) {
	t.Helper()

	_, err := db.Exec(`INSERT INTO materials (id, name) VALUES (1, 'PLA Pro')`)
	if err != nil {
		t.Fatalf("seed material: %v", err)
	}

	_, err = db.Exec(`
		INSERT INTO quotes (
			id, created_at, title, notes, waste_percent, margin_percent, tax_enabled, tax_percent_snapshot, totals_json, breakdown_json
		) VALUES (
			1,
			'2024-02-01 14:00:00',
			'Cotización Demo',
			'Entregar en 48h',
			7,
			35,
			1,
			19,
			'{"total":999.99}',
			'{"material_cost":123.45,"machine_cost":210.00,"labor_cost":50,"subtotal":383.45,"overhead":15,"failure_insurance":7.5,"packaging_cost":10,"shipping_cost":20,"margin":140,"tax":174.04}'
		)
	`)
	if err != nil {
		t.Fatalf("seed quote: %v", err)
	}

	_, err = db.Exec(`
		INSERT INTO quote_items (id, quote_id, material_id, grams, print_minutes, labor_minutes, quantity)
		VALUES (1, 1, 1, 150, 90, 12, 3)
	`)
	if err != nil {
		t.Fatalf("seed quote item: %v", err)
	}
}
