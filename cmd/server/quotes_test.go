package main

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestListQuotesOrdersByDateDescAndReadsTotal(t *testing.T) {
	db := newQuotesTestDB(t)
	srv := &server{db: db}

	seedQuote(t, db, "2024-01-01 10:00:00", "Primera", "nota uno", `{"total": 100.50}`)
	seedQuote(t, db, "2024-01-03 12:00:00", "Tercera", "nota tres", `{"total": 300.00}`)
	seedQuote(t, db, "2024-01-02 11:00:00", "Segunda", "nota dos", `{"total": 200.25}`)

	quotes, err := srv.listQuotes("")
	if err != nil {
		t.Fatalf("listQuotes returned error: %v", err)
	}

	if len(quotes) != 3 {
		t.Fatalf("expected 3 quotes, got %d", len(quotes))
	}

	if quotes[0].Title != "Tercera" || quotes[1].Title != "Segunda" || quotes[2].Title != "Primera" {
		t.Fatalf("quotes are not sorted desc by created_at: %+v", quotes)
	}

	if quotes[0].Total != 300.00 || quotes[1].Total != 200.25 || quotes[2].Total != 100.50 {
		t.Fatalf("unexpected totals: %+v", quotes)
	}
}

func TestListQuotesFilterByTitleAndNotes(t *testing.T) {
	db := newQuotesTestDB(t)
	srv := &server{db: db}

	seedQuote(t, db, "2024-01-01 10:00:00", "Casa", "impresión roja", `{"total": 80}`)
	seedQuote(t, db, "2024-01-02 10:00:00", "Llaveros", "cliente vip", `{"total": 120}`)
	seedQuote(t, db, "2024-01-03 10:00:00", "Prototipo", "urgente para casa", `{"total": 160}`)

	byTitle, err := srv.listQuotes("Llave")
	if err != nil {
		t.Fatalf("listQuotes title filter returned error: %v", err)
	}
	if len(byTitle) != 1 || byTitle[0].Title != "Llaveros" {
		t.Fatalf("expected 1 quote filtered by title, got %+v", byTitle)
	}

	byNotes, err := srv.listQuotes("casa")
	if err != nil {
		t.Fatalf("listQuotes notes filter returned error: %v", err)
	}
	if len(byNotes) != 2 {
		t.Fatalf("expected 2 quotes filtered by notes/title, got %+v", byNotes)
	}
}

func newQuotesTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE quotes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at DATETIME NOT NULL,
			title TEXT,
			notes TEXT,
			totals_json TEXT NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("failed creating quotes table: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}

func seedQuote(t *testing.T, db *sql.DB, createdAt, title, notes, totalsJSON string) {
	t.Helper()

	_, err := db.Exec(`
		INSERT INTO quotes (created_at, title, notes, totals_json)
		VALUES (?, ?, ?, ?)
	`, createdAt, title, notes, totalsJSON)
	if err != nil {
		t.Fatalf("failed to seed quote: %v", err)
	}
}
