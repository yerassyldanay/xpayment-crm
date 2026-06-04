package adminui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthCheck(t *testing.T) {
	a := newAuth("admin", "secret", "sessionkey")
	if !a.check("admin", "secret") {
		t.Fatal("valid credentials should pass")
	}
	if a.check("admin", "wrong") {
		t.Fatal("wrong password must fail")
	}
	if a.check("root", "secret") {
		t.Fatal("wrong user must fail")
	}
}

func TestSessionRoundTrip(t *testing.T) {
	a := newAuth("admin", "secret", "sessionkey")
	rec := httptest.NewRecorder()
	a.issue(rec)

	r := httptest.NewRequest(http.MethodGet, "/admin", nil)
	for _, c := range rec.Result().Cookies() {
		r.AddCookie(c)
	}
	if !a.valid(r) {
		t.Fatal("issued session cookie should validate")
	}
}

func TestForgedSessionRejected(t *testing.T) {
	a := newAuth("admin", "secret", "sessionkey")
	r := httptest.NewRequest(http.MethodGet, "/admin", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "admin|2026-01-01T00:00:00Z.deadbeef"})
	if a.valid(r) {
		t.Fatal("a cookie with a bad signature must be rejected")
	}
}

func TestAttackerWithDifferentSecretRejected(t *testing.T) {
	issuer := newAuth("admin", "secret", "real-secret")
	rec := httptest.NewRecorder()
	issuer.issue(rec)

	// A server with a different SESSION_SECRET must reject the cookie.
	verifier := newAuth("admin", "secret", "other-secret")
	r := httptest.NewRequest(http.MethodGet, "/admin", nil)
	for _, c := range rec.Result().Cookies() {
		r.AddCookie(c)
	}
	if verifier.valid(r) {
		t.Fatal("cookie signed with a different secret must not validate")
	}
}
