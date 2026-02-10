package config

import (
	"bufio"
	"errors"
	"os"
	"strings"
)

// loadDotEnv loads KEY=VALUE pairs from a dotenv file into the process environment.
// It is intentionally minimal: enough for local development without adding dependencies.
//
// Rules:
// - Empty lines and lines starting with # are ignored.
// - "export KEY=VALUE" is supported.
// - Values may be wrapped in single or double quotes; quotes are stripped.
// - Existing environment variables are not overwritten.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" {
			continue
		}

		// Strip surrounding quotes.
		if len(v) >= 2 {
			if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
				v = v[1 : len(v)-1]
			}
		}

		if os.Getenv(k) != "" {
			continue
		}
		_ = os.Setenv(k, v)
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return nil
}
