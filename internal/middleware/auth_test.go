package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "test-jwt-secret"

// makeToken is a test helper that signs a JWT with the given claims.
// It lets individual tests control the UserID and expiry without repeating
// the signing boilerplate.
func makeToken(t *testing.T, userID uint, expiry time.Time) string {
	t.Helper()
	claims := jwtClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiry),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	str, err := token.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("makeToken: failed to sign: %v", err)
	}
	return str
}

// dummyHandler is a minimal http.Handler used as the "next" in the middleware
// chain.  It simply records whether it was called.
func dummyHandler(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestRequireAuth_ValidToken(t *testing.T) {
	called := false
	mw := RequireAuth(testSecret)(dummyHandler(&called))

	token := makeToken(t, 7, time.Now().Add(time.Hour))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	mw.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !called {
		t.Error("expected next handler to be called, but it was not")
	}
}

func TestRequireAuth_ValidToken_UserIDInContext(t *testing.T) {
	var capturedID uint
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = UserIDFromContext(r.Context())
	})
	mw := RequireAuth(testSecret)(next)

	token := makeToken(t, 99, time.Now().Add(time.Hour))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	mw.ServeHTTP(w, req)

	if capturedID != 99 {
		t.Errorf("expected UserID=99 in context, got %d", capturedID)
	}
}

func TestRequireAuth_MissingHeader(t *testing.T) {
	called := false
	mw := RequireAuth(testSecret)(dummyHandler(&called))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	mw.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	if called {
		t.Error("next handler should NOT be called when header is missing")
	}
}

func TestRequireAuth_MissingBearerPrefix(t *testing.T) {
	called := false
	mw := RequireAuth(testSecret)(dummyHandler(&called))

	token := makeToken(t, 1, time.Now().Add(time.Hour))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// Intentionally missing the "Bearer " prefix.
	req.Header.Set("Authorization", token)
	w := httptest.NewRecorder()

	mw.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	if called {
		t.Error("next handler should NOT be called without Bearer prefix")
	}
}

func TestRequireAuth_ExpiredToken(t *testing.T) {
	called := false
	mw := RequireAuth(testSecret)(dummyHandler(&called))

	// Expiry in the past → token is expired.
	token := makeToken(t, 1, time.Now().Add(-time.Hour))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	mw.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for expired token, got %d", w.Code)
	}
	if called {
		t.Error("next handler should NOT be called with an expired token")
	}
}

func TestRequireAuth_TamperedToken(t *testing.T) {
	called := false
	mw := RequireAuth(testSecret)(dummyHandler(&called))

	// Sign with a different secret — our middleware will reject it.
	claims := jwtClaims{
		UserID: 1,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, _ := token.SignedString([]byte("wrong-secret"))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()

	mw.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for tampered token, got %d", w.Code)
	}
	if called {
		t.Error("next handler should NOT be called with a tampered token")
	}
}
