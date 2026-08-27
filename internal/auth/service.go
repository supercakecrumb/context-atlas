// Package auth implements Context Atlas's owner-only Telegram login flow.
package auth

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	authkit "github.com/supercakecrumb/msgr-authkit"
)

const (
	// LoginLinkTTL is the lifetime of a Telegram-issued browser login link.
	LoginLinkTTL = 10 * time.Minute
	// SessionTTL is the lifetime of an owner browser session.
	SessionTTL = 7 * 24 * time.Hour
)

// Service combines authkit's bot-first service with the session store needed
// by HTTP middleware and logout handling.
type Service struct {
	*authkit.AuthService
	Sessions *PGSessionIssuer
}

// NewService builds the owner-only login service. The Telegram adapter supplies
// the verified owner Telegram ID as SubjectID, so account links are unnecessary.
func NewService(pool *pgxpool.Pool, publicBaseURL string, sessionKey []byte) (*Service, error) {
	if pool == nil {
		return nil, fmt.Errorf("auth: database pool is required")
	}
	issuer, err := NewPGSessionIssuer(pool, SessionTTL)
	if err != nil {
		return nil, err
	}
	service, err := newAuthService(NewPGIntentStore(pool), issuer, publicBaseURL, sessionKey)
	if err != nil {
		return nil, err
	}
	return &Service{AuthService: service, Sessions: issuer}, nil
}

func newAuthService(intents authkit.IntentStore, sessions authkit.SessionIssuer, publicBaseURL string, sessionKey []byte) (*authkit.AuthService, error) {
	if len(sessionKey) < 32 {
		return nil, fmt.Errorf("auth: session key must contain at least 32 bytes")
	}
	loginURL, err := loginCallbackURL(publicBaseURL)
	if err != nil {
		return nil, err
	}
	return authkit.NewAuthService(
		intents,
		nil,
		sessions,
		authkit.WithConfig(authkit.Config{
			DefaultIntentTTL:      LoginLinkTTL,
			DefaultRedemptionMode: authkit.IntentOneTime,
			DefaultLoginLinkTTL:   LoginLinkTTL,
		}),
		authkit.WithSignedQueryLoginLinks(loginURL, sessionKey, "auth_token"),
	)
}

func loginCallbackURL(publicBaseURL string) (string, error) {
	policy, err := parsePublicBaseURL(publicBaseURL)
	if err != nil {
		return "", err
	}
	return policy.loginURL, nil
}

type publicURLPolicy struct {
	origin       string
	loginURL     string
	secureCookie bool
}

func parsePublicBaseURL(raw string) (publicURLPolicy, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil {
		return publicURLPolicy{}, fmt.Errorf("auth: PUBLIC_BASE_URL must be an absolute public URL")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return publicURLPolicy{}, fmt.Errorf("auth: PUBLIC_BASE_URL must use http or https")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return publicURLPolicy{}, fmt.Errorf("auth: PUBLIC_BASE_URL must include a host")
	}
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	canonicalHost := host
	if strings.Contains(host, ":") {
		canonicalHost = "[" + host + "]"
	}
	if port != "" {
		canonicalHost = net.JoinHostPort(strings.Trim(canonicalHost, "[]"), port)
	}
	u.Host = canonicalHost

	secureCookie := u.Scheme == "https"
	if !secureCookie && !loopbackHost(host) {
		return publicURLPolicy{}, fmt.Errorf("auth: HTTP PUBLIC_BASE_URL is allowed only on a loopback host")
	}
	origin := u.Scheme + "://" + u.Host
	u.RawQuery = ""
	u.Fragment = ""
	u.Path = strings.TrimRight(u.Path, "/") + "/login"
	return publicURLPolicy{origin: origin, loginURL: u.String(), secureCookie: secureCookie}, nil
}

func loopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
