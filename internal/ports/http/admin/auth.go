package adminui

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"time"
)

const sessionCookie = "xpayment_admin"

// auth holds the same-service login config (docs/08 · auth). No cross-service auth.
type auth struct {
	user     string
	passHash [32]byte
	secret   []byte
}

func newAuth(user, password, sessionSecret string) *auth {
	return &auth{user: user, passHash: sha256.Sum256([]byte(password)), secret: []byte(sessionSecret)}
}

// check verifies a login attempt in constant time.
func (a *auth) check(user, password string) bool {
	got := sha256.Sum256([]byte(password))
	userOK := subtle.ConstantTimeCompare([]byte(user), []byte(a.user)) == 1
	passOK := subtle.ConstantTimeCompare(got[:], a.passHash[:]) == 1
	return userOK && passOK
}

// issue writes a signed session cookie.
func (a *auth) issue(w http.ResponseWriter) {
	val := a.user + "|" + time.Now().UTC().Format(time.RFC3339)
	sig := a.sign(val)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    val + "." + sig,
		Path:     "/admin",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   12 * 3600,
	})
}

func (a *auth) clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/admin", MaxAge: -1})
}

// valid reports whether the request carries a well-signed session cookie.
func (a *auth) valid(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	dot := lastIndexByte(c.Value, '.')
	if dot < 0 {
		return false
	}
	val, sig := c.Value[:dot], c.Value[dot+1:]
	return hmac.Equal([]byte(sig), []byte(a.sign(val)))
}

func (a *auth) sign(val string) string {
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte(val))
	return hex.EncodeToString(mac.Sum(nil))
}

func lastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// requireAuth gates handlers behind a valid session, redirecting to login.
func (a *auth) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.valid(r) {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}
