package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv_LoadsValuesAndIgnoresNoise(t *testing.T) {
	t.Setenv("A", "")
	t.Setenv("B", "")
	t.Setenv("C", "")

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := []byte(`
# comment

A=one
export B=two
C="three"
`)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write dotenv: %v", err)
	}

	if err := loadDotEnv(path); err != nil {
		t.Fatalf("loadDotEnv: %v", err)
	}

	if got := os.Getenv("A"); got != "one" {
		t.Fatalf("A=%q, want %q", got, "one")
	}
	if got := os.Getenv("B"); got != "two" {
		t.Fatalf("B=%q, want %q", got, "two")
	}
	if got := os.Getenv("C"); got != "three" {
		t.Fatalf("C=%q, want %q", got, "three")
	}
}

func TestLoadDotEnv_DoesNotOverwriteExistingEnv(t *testing.T) {
	t.Setenv("KEEP", "already")

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("KEEP=fromfile\n"), 0o600); err != nil {
		t.Fatalf("write dotenv: %v", err)
	}

	if err := loadDotEnv(path); err != nil {
		t.Fatalf("loadDotEnv: %v", err)
	}

	if got := os.Getenv("KEEP"); got != "already" {
		t.Fatalf("KEEP=%q, want %q", got, "already")
	}
}

func TestLoadDotEnv_StripsSingleQuotes(t *testing.T) {
	t.Setenv("Q", "")

	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("Q='hello world'\n"), 0o600); err != nil {
		t.Fatalf("write dotenv: %v", err)
	}

	if err := loadDotEnv(path); err != nil {
		t.Fatalf("loadDotEnv: %v", err)
	}

	if got := os.Getenv("Q"); got != "hello world" {
		t.Fatalf("Q=%q, want %q", got, "hello world")
	}
}
