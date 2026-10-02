package handler

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestIssueUserTokenSetsSubjectAndExpiry(t *testing.T) {
	const secret = "test-secret"
	token, err := issueUserToken(secret, "buyer-1", time.Hour)
	if err != nil {
		t.Fatalf("issue user token: %v", err)
	}

	claims := &jwt.RegisteredClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		t.Fatalf("parse issued token: %v", err)
	}
	if !parsed.Valid || claims.Subject != "buyer-1" {
		t.Fatalf("expected signed token subject buyer-1, got valid=%t sub=%q", parsed.Valid, claims.Subject)
	}
	if claims.ExpiresAt == nil || claims.ExpiresAt.Time.Before(time.Now().Add(59*time.Minute)) {
		t.Fatalf("expected token expiry approximately one hour in the future, got %v", claims.ExpiresAt)
	}
}
