package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/paytm-hack/seatbooking/pkg/reqctx"
)

func TestRequireUserSetsIdentityFromJWTSubject(t *testing.T) {
	const secret = "test-secret"
	var userID string
	next := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		userID = reqctx.UserID(request.Context())
		writer.WriteHeader(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/shows/show-id/reserve", nil)
	request.Header.Set("Authorization", "Bearer "+signedUserToken(t, secret, "buyer-1", time.Now().Add(time.Minute)))
	response := httptest.NewRecorder()

	RequireUser(secret)(next).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, response.Code)
	}
	if userID != "buyer-1" {
		t.Fatalf("expected identity from JWT subject, got %q", userID)
	}
}

func TestRequireUserRejectsExpiredToken(t *testing.T) {
	const secret = "test-secret"
	request := httptest.NewRequest(http.MethodPost, "/shows/show-id/reserve", nil)
	request.Header.Set("Authorization", "Bearer "+signedUserToken(t, secret, "buyer-1", time.Now().Add(-time.Minute)))
	response := httptest.NewRecorder()

	RequireUser(secret)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("handler must not run for an expired token")
	})).ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, response.Code)
	}
}

func TestRequireAdminChecksBearerToken(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/shows", nil)
	request.Header.Set("Authorization", "Bearer admin-secret")
	response := httptest.NewRecorder()
	called := false

	RequireAdmin("admin-secret")(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		called = true
		writer.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent || !called {
		t.Fatalf("expected authorized handler call, got status %d", response.Code)
	}
}

func signedUserToken(t *testing.T, secret, userID string, expiresAt time.Time) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   userID,
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	})
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign JWT: %v", err)
	}
	return signed
}
