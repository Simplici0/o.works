package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const sessionCookieName = "oworks_session"

type authService struct {
	db            *sql.DB
	sessionSecret []byte
}

func newAuthService(db *sql.DB, sessionSecret string) *authService {
	return &authService{db: db, sessionSecret: []byte(sessionSecret)}
}

func (a *authService) validateCredentials(email, password string) (bool, error) {
	var passwordHash string
	err := a.db.QueryRow(`SELECT password_hash FROM users WHERE email = ?`, email).Scan(&passwordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query user credentials: %w", err)
	}

	providedHash := hashPassword(password)
	if subtle.ConstantTimeCompare([]byte(passwordHash), []byte(providedHash)) == 1 {
		return true, nil
	}

	// Compatibilidad: aceptar contraseñas almacenadas en texto plano.
	if subtle.ConstantTimeCompare([]byte(passwordHash), []byte(password)) == 1 {
		return true, nil
	}

	return false, nil
}

func (a *authService) ensureAdminUser(email, password string) error {
	if email == "" || password == "" {
		return nil
	}

	var exists bool
	err := a.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE email = ?)`, email).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check admin user existence: %w", err)
	}
	if exists {
		return nil
	}

	if _, err := a.db.Exec(`INSERT INTO users (email, password_hash) VALUES (?, ?)`, email, hashPassword(password)); err != nil {
		return fmt.Errorf("insert admin user: %w", err)
	}

	return nil
}

func hashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}

func (a *authService) createSessionValue(email string) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(email))
	mac := hmac.New(sha256.New, a.sessionSecret)
	_, _ = mac.Write([]byte(payload))
	signature := hex.EncodeToString(mac.Sum(nil))
	return payload + "." + signature
}

func (a *authService) verifySessionValue(value string) (string, bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return "", false
	}

	payload := parts[0]
	signature := parts[1]

	mac := hmac.New(sha256.New, a.sessionSecret)
	_, _ = mac.Write([]byte(payload))
	expected := mac.Sum(nil)

	provided, err := hex.DecodeString(signature)
	if err != nil {
		return "", false
	}
	if !hmac.Equal(provided, expected) {
		return "", false
	}

	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", false
	}
	if len(decoded) == 0 {
		return "", false
	}

	return string(decoded), true
}

func (a *authService) setSessionCookie(w http.ResponseWriter, email string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    a.createSessionValue(email),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *authService) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
