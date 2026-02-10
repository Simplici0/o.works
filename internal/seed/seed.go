package seed

import (
	"database/sql"
	"fmt"
	"os/exec"
	"strings"
)

const (
	defaultMaterialName    = "PLA (Genérico)"
	defaultPackagingName   = "Empaque estándar"
	defaultShippingCountry = "Colombia"
	defaultShippingCity    = "Bogotá"
)

// Config contains the values required by startup seed.
type Config struct {
	AdminEmail    string
	AdminPassword string
}

// Stats contains seed operation counters.
type Stats struct {
	Inserts int
	Updates int
}

// Run executes the startup seed in an idempotent way.
func Run(db *sql.DB, cfg Config) (Stats, error) {
	tx, err := db.Begin()
	if err != nil {
		return Stats{}, fmt.Errorf("begin seed transaction: %w", err)
	}

	stats := Stats{}

	if err := seedAdmin(tx, cfg.AdminEmail, cfg.AdminPassword, &stats); err != nil {
		_ = tx.Rollback()
		return Stats{}, err
	}
	if err := ensureMaterial(tx, &stats); err != nil {
		_ = tx.Rollback()
		return Stats{}, err
	}
	if err := ensureRateConfig(tx, &stats); err != nil {
		_ = tx.Rollback()
		return Stats{}, err
	}
	if err := ensurePackaging(tx, &stats); err != nil {
		_ = tx.Rollback()
		return Stats{}, err
	}
	if err := ensureShippingCO(tx, &stats); err != nil {
		_ = tx.Rollback()
		return Stats{}, err
	}

	if err := tx.Commit(); err != nil {
		return Stats{}, fmt.Errorf("commit seed transaction: %w", err)
	}

	return stats, nil
}

func seedAdmin(tx *sql.Tx, email, password string, stats *Stats) error {
	if email == "" || password == "" {
		return nil
	}

	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE email = ? LIMIT 1)`, email).Scan(&exists); err != nil {
		return fmt.Errorf("check admin user existence: %w", err)
	}
	if exists {
		return nil
	}

	hash, err := generateBcryptHash(password)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}

	if _, err := tx.Exec(`INSERT INTO users (email, password_hash) VALUES (?, ?)`, email, hash); err != nil {
		return fmt.Errorf("insert admin user: %w", err)
	}
	stats.Inserts++
	return nil
}

func generateBcryptHash(password string) (string, error) {
	cmd := exec.Command("python3", "-c", `import crypt,sys; print(crypt.crypt(sys.argv[1], crypt.mksalt(crypt.METHOD_BLOWFISH)))`, password)
	cmd.Env = append(cmd.Environ(), "PYTHONWARNINGS=ignore")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("generate bcrypt with python crypt: %w", err)
	}

	hash := strings.TrimSpace(string(out))
	if hash == "" || !strings.HasPrefix(hash, "$2") {
		return "", fmt.Errorf("unexpected bcrypt hash output")
	}

	return hash, nil
}

func ensureMaterial(tx *sql.Tx, stats *Stats) error {
	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM materials WHERE name = ? LIMIT 1)`, defaultMaterialName).Scan(&exists); err != nil {
		return fmt.Errorf("check default material existence: %w", err)
	}
	if exists {
		return nil
	}

	if _, err := tx.Exec(`
		INSERT INTO materials (name, cost_per_kg, notes, active)
		VALUES (?, ?, ?, ?)
	`, defaultMaterialName, 0, "", true); err != nil {
		return fmt.Errorf("insert default material: %w", err)
	}
	stats.Inserts++
	return nil
}

func ensureRateConfig(tx *sql.Tx, stats *Stats) error {
	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM rate_config WHERE id = 1)`).Scan(&exists); err != nil {
		return fmt.Errorf("check rate config existence: %w", err)
	}
	if exists {
		return nil
	}

	if _, err := tx.Exec(`
		INSERT INTO rate_config (
			id,
			machine_hourly_rate,
			labor_per_minute,
			overhead_fixed,
			overhead_percent,
			failure_rate_percent,
			tax_percent,
			currency
		)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?)
	`, 0, 0, 0, 0, 0, 19, "COP"); err != nil {
		return fmt.Errorf("insert rate config singleton: %w", err)
	}
	stats.Inserts++
	return nil
}

func ensurePackaging(tx *sql.Tx, stats *Stats) error {
	var exists bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM packaging_rates WHERE name = ? LIMIT 1)`, defaultPackagingName).Scan(&exists); err != nil {
		return fmt.Errorf("check packaging existence: %w", err)
	}
	if exists {
		return nil
	}

	if _, err := tx.Exec(`
		INSERT INTO packaging_rates (name, flat_cost, notes, active)
		VALUES (?, ?, ?, ?)
	`, defaultPackagingName, 0, "", true); err != nil {
		return fmt.Errorf("insert default packaging: %w", err)
	}
	stats.Inserts++
	return nil
}

func ensureShippingCO(tx *sql.Tx, stats *Stats) error {
	var exists bool
	if err := tx.QueryRow(`
		SELECT EXISTS(
			SELECT 1
			FROM shipping_rates
			WHERE country = ? AND city = ?
			LIMIT 1
		)
	`, defaultShippingCountry, defaultShippingCity).Scan(&exists); err != nil {
		return fmt.Errorf("check shipping rate existence: %w", err)
	}
	if exists {
		return nil
	}

	if _, err := tx.Exec(`
		INSERT INTO shipping_rates (scope, country, city, flat_cost, notes, active)
		VALUES (?, ?, ?, ?, ?, ?)
	`, "CITY", defaultShippingCountry, defaultShippingCity, 0, "", true); err != nil {
		return fmt.Errorf("insert default shipping rate: %w", err)
	}
	stats.Inserts++
	return nil
}
