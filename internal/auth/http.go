package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	authkit "github.com/supercakecrumb/msgr-authkit"
)

const (
	// SessionCookieName carries the opaque browser bearer token.
	SessionCookieName = "context_atlas_session"
	// CSRFHeaderName carries the per-session HMAC CSRF token.
	CSRFHeaderName = "X-CSRF-Token"
)

type contextKey struct{}

// SessionValidator is satisfied by authkit.SessionIssuer implementations.
type SessionValidator interface {
	Validate(context.Context, string) (authkit.WebSession, error)
}

// SessionRevoker invalidates a browser session during logout.
type SessionRevoker interface {
	Revoke(context.Context, string) error
}

// Handler provides login, session, and logout handlers plus middleware for
// owner-only admin routes.
type Handler struct {
	redeemer       loginRedeemer
	sessions       SessionValidator
	revoker        SessionRevoker
	csrfKey        []byte
	origin         string
	secureCookie   bool
	ownerSubjectID string
}

type loginRedeemer interface {
	RedeemLoginLink(context.Context, authkit.RedeemLoginLinkInput) (authkit.WebSession, error)
}

// NewHandler validates the dependencies shared by the owner auth handlers.
func NewHandler(redeemer loginRedeemer, sessions SessionValidator, revoker SessionRevoker, csrfKey []byte, publicBaseURL, ownerSubjectID string) (*Handler, error) {
	if redeemer == nil || sessions == nil || revoker == nil {
		return nil, fmt.Errorf("auth: login and session services are required")
	}
	if len(csrfKey) < 32 {
		return nil, fmt.Errorf("auth: csrf key must contain at least 32 bytes")
	}
	policy, err := parsePublicBaseURL(publicBaseURL)
	if err != nil {
		return nil, err
	}
	ownerSubjectID = strings.TrimSpace(ownerSubjectID)
	if ownerSubjectID == "" {
		return nil, fmt.Errorf("auth: owner subject ID is required")
	}
	key := append([]byte(nil), csrfKey...)
	return &Handler{redeemer: redeemer, sessions: sessions, revoker: revoker, csrfKey: key, origin: policy.origin, secureCookie: policy.secureCookie, ownerSubjectID: ownerSubjectID}, nil
}

// Login returns a handler for /login. Requests without a token continue to
// fallback so the SPA can render its Telegram instructions; token-bearing GETs
// redeem the one-time link before redirecting to /admin.
func (h *Handler) Login(fallback http.Handler) http.Handler {
	if fallback == nil {
		fallback = http.NotFoundHandler()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Query().Get("auth_token") == "" {
			fallback.ServeHTTP(w, r)
			return
		}
		h.RedeemLogin(w, r)
	})
}

// RedeemLogin exchanges an auth_token query parameter for a browser session.
func (h *Handler) RedeemLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	token := r.URL.Query().Get("auth_token")
	if token == "" {
		writeError(w, http.StatusBadRequest, "missing login token")
		return
	}
	session, err := h.redeemer.RedeemLoginLink(r.Context(), authkit.RedeemLoginLinkInput{LinkToken: token})
	if err != nil || session.SubjectID != h.ownerSubjectID {
		writeError(w, http.StatusBadRequest, "invalid or expired login link")
		return
	}
	setSessionCookie(w, session, h.secureCookie)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// Session reports the owner login state and a CSRF token when authenticated.
func (h *Handler) Session(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	session, ok := h.sessionFromRequest(r)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if !ok {
		_ = json.NewEncoder(w).Encode(struct {
			Authenticated bool `json:"authenticated"`
		}{Authenticated: false})
		return
	}
	_ = json.NewEncoder(w).Encode(struct {
		Authenticated bool   `json:"authenticated"`
		CSRFToken     string `json:"csrf_token"`
	}{Authenticated: true, CSRFToken: CSRFTokenFor(h.csrfKey, session.Token)})
}

// Logout revokes the current session and clears its cookie. It protects itself
// so it is safe to wire directly; RequireAdmin is for other admin endpoints.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		methodNotAllowed(w)
		return
	}
	session, ok := h.sessionFromRequest(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if !h.validUnsafeRequest(r, session) {
		writeError(w, http.StatusForbidden, "invalid csrf request")
		return
	}
	if err := h.revoker.Revoke(r.Context(), session.Token); err != nil {
		writeError(w, http.StatusInternalServerError, "unable to end session")
		return
	}
	clearSessionCookie(w, h.secureCookie)
	w.WriteHeader(http.StatusNoContent)
}

// RequireSession protects a route with the current owner session and stores it
// in the request context. It is useful for safe owner-only reads.
func (h *Handler) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := h.sessionFromRequest(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		ctx := context.WithValue(r.Context(), contextKey{}, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin adds exact Origin/Referer and HMAC-CSRF protection to unsafe
// owner requests on top of RequireSession.
func (h *Handler) RequireAdmin(next http.Handler) http.Handler {
	return h.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, _ := SessionFromContext(r.Context())
		if !h.validUnsafeRequest(r, session) {
			writeError(w, http.StatusForbidden, "invalid csrf request")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// SessionFromContext returns the session installed by RequireAdmin.
func SessionFromContext(ctx context.Context) (authkit.WebSession, bool) {
	session, ok := ctx.Value(contextKey{}).(authkit.WebSession)
	return session, ok
}

func (h *Handler) sessionFromRequest(r *http.Request) (authkit.WebSession, bool) {
	if session, ok := SessionFromContext(r.Context()); ok && session.SubjectID == h.ownerSubjectID {
		return session, true
	}
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return authkit.WebSession{}, false
	}
	session, err := h.sessions.Validate(r.Context(), cookie.Value)
	if err != nil || session.SubjectID != h.ownerSubjectID {
		return authkit.WebSession{}, false
	}
	return session, true
}

func (h *Handler) validUnsafeRequest(r *http.Request, session authkit.WebSession) bool {
	if !unsafeMethod(r.Method) {
		return true
	}
	if !sameOrigin(r, h.origin) {
		return false
	}
	return VerifyCSRF(h.csrfKey, session.Token, r.Header.Get(CSRFHeaderName))
}

func setSessionCookie(w http.ResponseWriter, session authkit.WebSession, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    session.Token,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// CSRFTokenFor derives a deterministic, per-session CSRF token.
func CSRFTokenFor(key []byte, sessionToken string) string {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(sessionToken))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyCSRF compares a supplied CSRF token without timing leaks.
func VerifyCSRF(key []byte, sessionToken, supplied string) bool {
	return hmac.Equal([]byte(CSRFTokenFor(key, sessionToken)), []byte(supplied))
}

func unsafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	default:
		return true
	}
}

func sameOrigin(r *http.Request, expected string) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		return origin == expected
	}
	referer := r.Referer()
	if referer == "" {
		return false
	}
	u, err := url.Parse(referer)
	return err == nil && u.User == nil && u.Scheme+"://"+u.Host == expected
}

func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{Error: message})
}
