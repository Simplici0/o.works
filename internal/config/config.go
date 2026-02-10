package config

import (
	"log"
	"os"
)

const (
	defaultDBPath = "./dev.db"
	defaultPort   = "8080"
)

// Config holds application configuration sourced from environment variables.
type Config struct {
	AdminEmail    string
	AdminPassword string
	SessionSecret string
	DBPath        string
	Port          string
}

// Load reads environment variables and returns a populated Config.
func Load() Config {
	// Best-effort: load local dev environment variables.
	// We don't fail if the file is missing; production should use real env injection.
	_ = loadDotEnv(".env")

	cfg := Config{
		AdminEmail:    os.Getenv("ADMIN_EMAIL"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
		SessionSecret: os.Getenv("SESSION_SECRET"),
		DBPath:        os.Getenv("DB_PATH"),
		Port:          os.Getenv("PORT"),
	}

	if cfg.DBPath == "" {
		cfg.DBPath = defaultDBPath
	}
	if cfg.Port == "" {
		cfg.Port = defaultPort
	}

	if cfg.AdminEmail == "" {
		log.Print("warning: ADMIN_EMAIL is not set")
	}
	if cfg.AdminPassword == "" {
		log.Print("warning: ADMIN_PASSWORD is not set")
	}
	if cfg.SessionSecret == "" {
		log.Print("warning: SESSION_SECRET is not set")
	}

	return cfg
}
