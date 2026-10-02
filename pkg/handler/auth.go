package handler

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func issueUserToken(secret, userID string, lifetime time.Duration) (string, error) {
	if secret == "" || userID == "" || lifetime <= 0 {
		return "", jwt.ErrTokenInvalidClaims
	}

	now := time.Now().UTC()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(lifetime)),
	})
	return token.SignedString([]byte(secret))
}
