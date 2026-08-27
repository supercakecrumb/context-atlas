package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	authkit "github.com/supercakecrumb/msgr-authkit"
)

// PGIntentStore persists signed-login intents in PostgreSQL.
type PGIntentStore struct {
	pool *pgxpool.Pool
}

// NewPGIntentStore returns a PostgreSQL-backed authkit intent store.
func NewPGIntentStore(pool *pgxpool.Pool) *PGIntentStore {
	return &PGIntentStore{pool: pool}
}

type identityJSON struct {
	MessengerID     string            `json:"messenger_id"`
	MessengerUserID string            `json:"messenger_user_id"`
	Username        string            `json:"username,omitempty"`
	Name            string            `json:"name,omitempty"`
	Surname         string            `json:"surname,omitempty"`
	BirthDate       *time.Time        `json:"birth_date,omitempty"`
	Attributes      map[string]string `json:"attributes,omitempty"`
}

func marshalIdentity(identity *authkit.Identity) (any, error) {
	if identity == nil {
		return nil, nil
	}
	raw, err := json.Marshal(identityJSON{
		MessengerID:     identity.Messenger.ID,
		MessengerUserID: identity.MessengerUserID,
		Username:        identity.Username,
		Name:            identity.Name,
		Surname:         identity.Surname,
		BirthDate:       identity.BirthDate,
		Attributes:      identity.Attributes,
	})
	if err != nil {
		return nil, fmt.Errorf("auth: marshal identity: %w", err)
	}
	return string(raw), nil
}

func unmarshalIdentity(raw []byte) (*authkit.Identity, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var stored identityJSON
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, fmt.Errorf("auth: unmarshal identity: %w", err)
	}
	return &authkit.Identity{
		Messenger:       authkit.NewMessenger(stored.MessengerID),
		MessengerUserID: stored.MessengerUserID,
		Username:        stored.Username,
		Name:            stored.Name,
		Surname:         stored.Surname,
		BirthDate:       stored.BirthDate,
		Attributes:      stored.Attributes,
	}, nil
}

func marshalMetadata(metadata map[string]string) (any, error) {
	if len(metadata) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("auth: marshal intent metadata: %w", err)
	}
	return string(raw), nil
}

func unmarshalMetadata(raw []byte) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var metadata map[string]string
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return nil, fmt.Errorf("auth: unmarshal intent metadata: %w", err)
	}
	return metadata, nil
}

// Create implements authkit.IntentStore.
func (s *PGIntentStore) Create(ctx context.Context, intent authkit.AuthIntent) error {
	identity, err := marshalIdentity(intent.Identity)
	if err != nil {
		return err
	}
	metadata, err := marshalMetadata(intent.Metadata)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO auth_intents (
			id, code, messenger, audience, subject_id, state, identity_json,
			metadata_json, redemption_mode, max_redemptions, redemption_count,
			expires_at, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9, $10, $11, $12, $13
		)`,
		intent.ID,
		intent.Code,
		intent.Messenger.ID,
		intent.Audience,
		intent.SubjectID,
		string(intent.State),
		identity,
		metadata,
		string(intent.RedemptionMode),
		intent.MaxRedemptions,
		intent.RedemptionCount,
		intent.ExpiresAt,
		intent.CreatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("auth: create intent: %w", err)
	}
	return nil
}

// FindByCode implements authkit.IntentStore.
func (s *PGIntentStore) FindByCode(ctx context.Context, messenger authkit.Messenger, code string) (authkit.AuthIntent, error) {
	intent, err := scanIntent(s.pool.QueryRow(ctx, `
		SELECT id, code, messenger, audience, subject_id, state, identity_json,
		       metadata_json, redemption_mode, max_redemptions, redemption_count,
		       expires_at, created_at, consumed_at
		FROM auth_intents
		WHERE messenger = $1 AND code = $2`, messenger.ID, code))
	if errors.Is(err, pgx.ErrNoRows) {
		return authkit.AuthIntent{}, authkit.ErrIntentNotFound
	}
	if err != nil {
		return authkit.AuthIntent{}, fmt.Errorf("auth: find intent: %w", err)
	}
	return intent, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanIntent(row rowScanner) (authkit.AuthIntent, error) {
	var (
		intent                authkit.AuthIntent
		messenger, state      string
		mode                  string
		identity, metadata    []byte
		expiresAt, consumedAt pgtype.Timestamptz
	)
	if err := row.Scan(
		&intent.ID,
		&intent.Code,
		&messenger,
		&intent.Audience,
		&intent.SubjectID,
		&state,
		&identity,
		&metadata,
		&mode,
		&intent.MaxRedemptions,
		&intent.RedemptionCount,
		&expiresAt,
		&intent.CreatedAt,
		&consumedAt,
	); err != nil {
		return authkit.AuthIntent{}, err
	}
	decodedIdentity, err := unmarshalIdentity(identity)
	if err != nil {
		return authkit.AuthIntent{}, err
	}
	decodedMetadata, err := unmarshalMetadata(metadata)
	if err != nil {
		return authkit.AuthIntent{}, err
	}
	intent.Messenger = authkit.NewMessenger(messenger)
	intent.State = authkit.IntentState(state)
	intent.RedemptionMode = authkit.IntentRedemptionMode(mode)
	intent.Identity = decodedIdentity
	intent.Metadata = decodedMetadata
	intent.CreatedAt = intent.CreatedAt.UTC()
	if expiresAt.Valid {
		t := expiresAt.Time.UTC()
		intent.ExpiresAt = &t
	}
	if consumedAt.Valid {
		t := consumedAt.Time.UTC()
		intent.ConsumedAt = &t
	}
	return intent, nil
}

// RecordRedemption implements authkit.IntentStore. A row lock makes one-time
// link redemption atomic across concurrent browser requests.
func (s *PGIntentStore) RecordRedemption(ctx context.Context, intentID string, redeemedAt time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("auth: begin intent redemption: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		state, mode                     string
		maxRedemptions, redemptionCount int
		expiresAt                       pgtype.Timestamptz
	)
	err = tx.QueryRow(ctx, `
		SELECT state, redemption_mode, max_redemptions, redemption_count, expires_at
		FROM auth_intents
		WHERE id = $1
		FOR UPDATE`, intentID).Scan(&state, &mode, &maxRedemptions, &redemptionCount, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return authkit.ErrIntentNotFound
	}
	if err != nil {
		return fmt.Errorf("auth: load intent for redemption: %w", err)
	}

	if state != string(authkit.IntentActive) {
		return authkit.ErrIntentNotActive
	}
	now := redeemedAt.UTC()
	if expiresAt.Valid && now.After(expiresAt.Time.UTC()) {
		if _, err := tx.Exec(ctx, `UPDATE auth_intents SET state = $1 WHERE id = $2`, string(authkit.IntentExpired), intentID); err != nil {
			return fmt.Errorf("auth: expire intent: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("auth: commit expired intent: %w", err)
		}
		return authkit.ErrIntentExpired
	}

	switch authkit.IntentRedemptionMode(mode) {
	case authkit.IntentOneTime:
		if redemptionCount >= 1 {
			return authkit.ErrIntentAlreadyRedeemed
		}
	case authkit.IntentReusable:
		if maxRedemptions > 0 && redemptionCount >= maxRedemptions {
			return authkit.ErrIntentRedemptionLimitReached
		}
	default:
		return fmt.Errorf("auth: unsupported redemption mode %q", mode)
	}

	redemptionCount++
	newState := state
	if mode == string(authkit.IntentOneTime) || (mode == string(authkit.IntentReusable) && maxRedemptions > 0 && redemptionCount >= maxRedemptions) {
		newState = string(authkit.IntentRevoked)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE auth_intents
		SET redemption_count = $1, consumed_at = $2, state = $3
		WHERE id = $4`, redemptionCount, now, newState, intentID); err != nil {
		return fmt.Errorf("auth: redeem intent: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("auth: commit intent redemption: %w", err)
	}
	return nil
}

// DeleteExpired implements authkit.IntentStore.
func (s *PGIntentStore) DeleteExpired(ctx context.Context, now time.Time) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM auth_intents WHERE expires_at IS NOT NULL AND expires_at < $1`, now.UTC())
	if err != nil {
		return fmt.Errorf("auth: delete expired intents: %w", err)
	}
	return nil
}

// PGSessionIssuer persists only SHA-256 hashes of opaque browser session
// tokens. It also implements Revoke for logout.
type PGSessionIssuer struct {
	pool *pgxpool.Pool
	ttl  time.Duration
}

// NewPGSessionIssuer returns a session issuer with the supplied positive TTL.
func NewPGSessionIssuer(pool *pgxpool.Pool, ttl time.Duration) (*PGSessionIssuer, error) {
	if pool == nil {
		return nil, fmt.Errorf("auth: database pool is required")
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("auth: session TTL must be positive")
	}
	return &PGSessionIssuer{pool: pool, ttl: ttl}, nil
}

// Issue implements authkit.SessionIssuer.
func (s *PGSessionIssuer) Issue(ctx context.Context, subjectID string) (authkit.WebSession, error) {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return authkit.WebSession{}, fmt.Errorf("auth: session subject is required")
	}
	token, err := randomToken(32)
	if err != nil {
		return authkit.WebSession{}, fmt.Errorf("auth: generate session token: %w", err)
	}
	now := time.Now().UTC()
	session := authkit.WebSession{
		SessionID: uuid.NewString(),
		SubjectID: subjectID,
		Token:     token,
		IssuedAt:  now,
		ExpiresAt: now.Add(s.ttl),
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO web_sessions (session_id, subject_id, token_hash, issued_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)`,
		session.SessionID,
		session.SubjectID,
		hashToken(session.Token),
		session.IssuedAt,
		session.ExpiresAt,
	)
	if err != nil {
		return authkit.WebSession{}, fmt.Errorf("auth: create session: %w", err)
	}
	return session, nil
}

// Validate implements authkit.SessionIssuer.
func (s *PGSessionIssuer) Validate(ctx context.Context, token string) (authkit.WebSession, error) {
	if strings.TrimSpace(token) == "" {
		return authkit.WebSession{}, authkit.ErrSessionNotFound
	}
	var session authkit.WebSession
	err := s.pool.QueryRow(ctx, `
		SELECT session_id, subject_id, issued_at, expires_at
		FROM web_sessions
		WHERE token_hash = $1`, hashToken(token)).Scan(
		&session.SessionID,
		&session.SubjectID,
		&session.IssuedAt,
		&session.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return authkit.WebSession{}, authkit.ErrSessionNotFound
	}
	if err != nil {
		return authkit.WebSession{}, fmt.Errorf("auth: validate session: %w", err)
	}
	if !session.ExpiresAt.After(time.Now().UTC()) {
		return authkit.WebSession{}, authkit.ErrSessionExpired
	}
	session.Token = token
	session.IssuedAt = session.IssuedAt.UTC()
	session.ExpiresAt = session.ExpiresAt.UTC()
	return session, nil
}

// Revoke removes a browser session by its plaintext bearer token.
func (s *PGSessionIssuer) Revoke(ctx context.Context, token string) error {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM web_sessions WHERE token_hash = $1`, hashToken(token)); err != nil {
		return fmt.Errorf("auth: revoke session: %w", err)
	}
	return nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func randomToken(bytes int) (string, error) {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
