package seed

import (
	"database/sql"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Simplici0/o.works/internal/db"
	"github.com/Simplici0/o.works/internal/migrations"
)

func TestRunIsIdempotent(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "seed-test.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	defer database.Close()

	if err := migrations.Up(database, "../../migrations"); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	cfg := Config{
		AdminEmail:    "admin@oworks.com",
		AdminPassword: "12345",
	}

	for i := 0; i < 10; i++ {
		stats, err := Run(database, cfg)
		if err != nil {
			t.Fatalf("run seed (iteration=%d): %v", i, err)
		}
		if i == 0 {
			if stats.Inserts != 5 {
				t.Fatalf("expected 5 inserts in first run, got %d", stats.Inserts)
			}
			continue
		}
		if stats.Inserts != 0 {
			t.Fatalf("expected 0 inserts in iteration %d, got %d", i, stats.Inserts)
		}
	}

	assertCount(t, database, `SELECT COUNT(*) FROM users WHERE email = ?`, "admin@oworks.com", 1)
	assertCount(t, database, `SELECT COUNT(*) FROM materials WHERE name = ?`, "PLA (Genérico)", 1)
	assertCount(t, database, `SELECT COUNT(*) FROM rate_config WHERE id = 1`, nil, 1)
	assertCount(t, database, `SELECT COUNT(*) FROM packaging_rates WHERE name = ?`, "Empaque estándar", 1)
	assertCount(t, database, `SELECT COUNT(*) FROM shipping_rates WHERE country = ? AND city = ?`, []any{"Colombia", "Bogotá"}, 1)

	var hash string
	if err := database.QueryRow(`SELECT password_hash FROM users WHERE email = ?`, "admin@oworks.com").Scan(&hash); err != nil {
		t.Fatalf("query admin hash: %v", err)
	}
	if !verifyBcryptWithPython(t, hash, "12345") {
		t.Fatalf("expected admin hash to match password")
	}
}

func verifyBcryptWithPython(t *testing.T, hash, password string) bool {
	t.Helper()

	cmd := exec.Command("python3", "-c", `import crypt,sys; print(crypt.crypt(sys.argv[2], sys.argv[1]))`, hash, password)
	cmd.Env = append(cmd.Environ(), "PYTHONWARNINGS=ignore")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("verify bcrypt hash with python: %v", err)
	}

	return strings.TrimSpace(string(out)) == hash
}

func assertCount(t *testing.T, database *sql.DB, query string, args any, expected int) {
	t.Helper()

	var count int
	var err error
	switch v := args.(type) {
	case nil:
		err = database.QueryRow(query).Scan(&count)
	case []any:
		err = database.QueryRow(query, v...).Scan(&count)
	default:
		err = database.QueryRow(query, v).Scan(&count)
	}
	if err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if count != expected {
		t.Fatalf("expected count %d, got %d", expected, count)
	}
}
