package auth

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidCredentials = errors.New("invalid credentials")

type Service struct {
	db                    *pgxpool.Pool
	secret                []byte
	accessTTL, refreshTTL time.Duration
	appleClientID         string
	mailer                func(string, string, string) error
	appleKeys             map[string]*rsa.PublicKey
	appleKeysAt           time.Time
	appleMu               sync.Mutex
}
type User struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
}

func (s *Service) BootstrapAdmin(ctx context.Context, email, password string) error {
	if email == "" || password == "" {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO users(id,email,password_hash,display_name,role) VALUES($1,$2,$3,$4,'admin') ON CONFLICT(email) DO UPDATE SET role='admin', password_hash=EXCLUDED.password_hash, updated_at=NOW()`, uuid.New(), strings.ToLower(strings.TrimSpace(email)), string(hash), "系统管理员")
	return err
}

func NewService(pool *pgxpool.Pool, secret string, appleClientID ...string) *Service {
	id := ""
	if len(appleClientID) > 0 {
		id = appleClientID[0]
	}
	return &Service{db: pool, secret: []byte(secret), accessTTL: 30 * time.Minute, refreshTTL: 30 * 24 * time.Hour, appleClientID: id, appleKeys: map[string]*rsa.PublicKey{}}
}

func (s *Service) appleKey(kid string) (*rsa.PublicKey, error) {
	s.appleMu.Lock()
	defer s.appleMu.Unlock()
	if key, ok := s.appleKeys[kid]; ok && time.Since(s.appleKeysAt) < time.Hour {
		return key, nil
	}
	resp, e := http.Get("https://appleid.apple.com/auth/keys")
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	var keys struct {
		Keys []struct {
			Kid string   `json:"kid"`
			X5c []string `json:"x5c"`
		} `json:"keys"`
	}
	if e = json.NewDecoder(resp.Body).Decode(&keys); e != nil {
		return nil, e
	}
	next := map[string]*rsa.PublicKey{}
	for _, k := range keys.Keys {
		if len(k.X5c) == 0 {
			continue
		}
		b, e := base64.StdEncoding.DecodeString(k.X5c[0])
		if e != nil {
			continue
		}
		cert, e := x509.ParseCertificate(b)
		if e == nil {
			if pub, ok := cert.PublicKey.(*rsa.PublicKey); ok {
				next[k.Kid] = pub
			}
		}
	}
	for key, value := range next {
		s.appleKeys[key] = value
	}
	s.appleKeysAt = time.Now()
	if key := s.appleKeys[kid]; key != nil {
		return key, nil
	}
	return nil, fmt.Errorf("apple key not found")
}

func (s *Service) AppleLogin(ctx context.Context, identityToken, name, nonce string) (User, string, string, error) {
	if s.appleClientID == "" {
		return User{}, "", "", ErrInvalidCredentials
	}
	parsed, err := jwt.Parse(identityToken, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != "RS256" {
			return nil, errors.New("invalid apple algorithm")
		}
		kid, _ := t.Header["kid"].(string)
		return s.appleKey(kid)
	})
	if err != nil || !parsed.Valid {
		return User{}, "", "", ErrInvalidCredentials
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok || claims["iss"] != "https://appleid.apple.com" || claims["aud"] != s.appleClientID {
		return User{}, "", "", ErrInvalidCredentials
	}
	if exp, ok := claims["exp"].(float64); !ok || time.Now().Unix() >= int64(exp) {
		return User{}, "", "", ErrInvalidCredentials
	}
	if iat, ok := claims["iat"].(float64); !ok || time.Now().Add(10*time.Minute).Unix() < int64(iat) {
		return User{}, "", "", ErrInvalidCredentials
	}
	if nonce == "" {
		return User{}, "", "", ErrInvalidCredentials
	}
	claimNonce, _ := claims["nonce"].(string)
	sum := sha256.Sum256([]byte(nonce))
	if claimNonce != nonce && claimNonce != fmt.Sprintf("%x", sum[:]) {
		return User{}, "", "", ErrInvalidCredentials
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return User{}, "", "", ErrInvalidCredentials
	}
	var u User
	err = s.db.QueryRow(ctx, `SELECT id,COALESCE(email,''),COALESCE(display_name,''),role FROM users WHERE apple_subject=$1 AND deleted_at IS NULL`, sub).Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		u = User{ID: uuid.New(), DisplayName: strings.TrimSpace(name), Role: "student"}
		if email, _ := claims["email"].(string); email != "" {
			u.Email = email
		}
		_, err = s.db.Exec(ctx, `INSERT INTO users(id,email,display_name,role,apple_subject) VALUES($1,NULLIF($2,''),$3,$4,$5)`, u.ID, u.Email, u.DisplayName, u.Role, sub)
	}
	if err != nil {
		return User{}, "", "", err
	}
	a, r, e := s.tokens(u)
	return u, a, r, e
}
func (s *Service) Register(ctx context.Context, email, password, name string) (User, string, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	if len(email) < 5 || len(password) < 8 {
		return User{}, "", "", ErrInvalidCredentials
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, "", "", err
	}
	u := User{ID: uuid.New(), Email: email, DisplayName: name, Role: "student"}
	if _, err = s.db.Exec(ctx, `INSERT INTO users (id,email,password_hash,display_name) VALUES ($1,$2,$3,$4)`, u.ID, u.Email, string(hash), u.DisplayName); err != nil {
		return User{}, "", "", err
	}
	a, r, err := s.tokens(u)
	return u, a, r, err
}

func (s *Service) SetMailer(fn func(string, string, string) error) { s.mailer = fn }
func (s *Service) issueToken(ctx context.Context, id uuid.UUID, purpose string) (string, error) {
	raw := uuid.NewString() + uuid.NewString()
	sum := sha256.Sum256([]byte(raw))
	_, err := s.db.Exec(ctx, `INSERT INTO account_tokens(id,user_id,token_hash,purpose,expires_at) VALUES($1,$2,$3,$4,NOW()+INTERVAL '30 minutes')`, uuid.New(), id, fmt.Sprintf("%x", sum[:]), purpose)
	if err == nil && s.mailer != nil {
		var email string
		if s.db.QueryRow(ctx, `SELECT email FROM users WHERE id=$1`, id).Scan(&email) == nil {
			subject := "K12Edu verification"
			if purpose == "password_reset" {
				subject = "K12Edu password reset"
			}
			_ = s.mailer(email, subject, raw)
		}
	}
	return raw, err
}
func (s *Service) VerifyEmail(ctx context.Context, raw string) error {
	return s.consumeToken(ctx, raw, "verify_email", `UPDATE users SET email_verified_at=NOW(),updated_at=NOW() WHERE id=$1`)
}
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	var id uuid.UUID
	if err := s.db.QueryRow(ctx, `SELECT id FROM users WHERE email=$1 AND deleted_at IS NULL`, strings.ToLower(strings.TrimSpace(email))).Scan(&id); err != nil {
		return nil
	}
	_, err := s.issueToken(ctx, id, "password_reset")
	return err
}
func (s *Service) ResetPassword(ctx context.Context, raw, password string) error {
	if len(password) < 8 {
		return ErrInvalidCredentials
	}
	sum := sha256.Sum256([]byte(raw))
	var id uuid.UUID
	if err := s.db.QueryRow(ctx, `UPDATE account_tokens SET used_at=NOW() WHERE token_hash=$1 AND purpose='password_reset' AND used_at IS NULL AND expires_at>NOW() RETURNING user_id`, fmt.Sprintf("%x", sum[:])).Scan(&id); err != nil {
		return ErrInvalidCredentials
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `UPDATE users SET password_hash=$2,updated_at=NOW() WHERE id=$1`, id, string(hash))
	return err
}
func (s *Service) consumeToken(ctx context.Context, raw, purpose, sql string) error {
	sum := sha256.Sum256([]byte(raw))
	var id uuid.UUID
	if err := s.db.QueryRow(ctx, `UPDATE account_tokens SET used_at=NOW() WHERE token_hash=$1 AND purpose=$2 AND used_at IS NULL AND expires_at>NOW() RETURNING user_id`, fmt.Sprintf("%x", sum[:]), purpose).Scan(&id); err != nil {
		return ErrInvalidCredentials
	}
	_, err := s.db.Exec(ctx, sql, id)
	return err
}
func (s *Service) Login(ctx context.Context, email, password string) (User, string, string, error) {
	var u User
	var hash string
	err := s.db.QueryRow(ctx, `SELECT id,email,display_name,role,password_hash FROM users WHERE email=$1 AND deleted_at IS NULL AND (locked_until IS NULL OR locked_until<NOW())`, strings.ToLower(strings.TrimSpace(email))).Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role, &hash)
	if errors.Is(err, pgx.ErrNoRows) || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		_, _ = s.db.Exec(ctx, `UPDATE users SET login_failures=login_failures+1,locked_until=CASE WHEN login_failures+1>=5 THEN NOW()+INTERVAL '15 minutes' ELSE locked_until END WHERE email=$1 AND deleted_at IS NULL`, strings.ToLower(strings.TrimSpace(email)))
		return User{}, "", "", ErrInvalidCredentials
	}
	_, _ = s.db.Exec(ctx, `UPDATE users SET login_failures=0,locked_until=NULL WHERE id=$1`, u.ID)
	a, r, err := s.tokens(u)
	return u, a, r, err
}
func (s *Service) tokens(u User) (string, string, error) {
	now := time.Now()
	mk := func(ttl time.Duration, typ string) (string, error) {
		return jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": u.ID.String(), "role": u.Role, "typ": typ, "iat": now.Unix(), "exp": now.Add(ttl).Unix()}).SignedString(s.secret)
	}
	a, e := mk(s.accessTTL, "access")
	if e != nil {
		return "", "", e
	}
	r, e := mk(s.refreshTTL, "refresh")
	return a, r, e
}
func (s *Service) Parse(raw string) (uuid.UUID, string, error) {
	t, e := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	})
	if e != nil || !t.Valid {
		return uuid.Nil, "", ErrInvalidCredentials
	}
	sub, e := t.Claims.GetSubject()
	if e != nil {
		return uuid.Nil, "", ErrInvalidCredentials
	}
	id, e := uuid.Parse(sub)
	if e != nil {
		return uuid.Nil, "", ErrInvalidCredentials
	}
	claims := t.Claims.(jwt.MapClaims)
	if typ, _ := claims["typ"].(string); typ != "access" {
		return uuid.Nil, "", ErrInvalidCredentials
	}
	role, _ := claims["role"].(string)
	return id, role, nil
}

func (s *Service) Refresh(raw string) (string, error) {
	if s.db != nil {
		sum := sha256.Sum256([]byte(raw))
		var active bool
		if err := s.db.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM auth_sessions WHERE refresh_token_hash=$1 AND revoked_at IS NULL AND expires_at>NOW())`, fmt.Sprintf("%x", sum[:])).Scan(&active); err == nil && !active {
			return "", ErrInvalidCredentials
		}
	}
	t, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	})
	if err != nil || !t.Valid {
		return "", ErrInvalidCredentials
	}
	claims, ok := t.Claims.(jwt.MapClaims)
	if !ok {
		return "", ErrInvalidCredentials
	}
	if typ, _ := claims["typ"].(string); typ != "refresh" {
		return "", ErrInvalidCredentials
	}
	sub, err := t.Claims.GetSubject()
	if err != nil {
		return "", ErrInvalidCredentials
	}
	id, err := uuid.Parse(sub)
	if err != nil {
		return "", ErrInvalidCredentials
	}
	var u User
	err = s.db.QueryRow(context.Background(), `SELECT id,email,display_name,role FROM users WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role)
	if err != nil {
		return "", ErrInvalidCredentials
	}
	a, _, err := s.tokens(u)
	return a, err
}

func (s *Service) RevokeRefresh(ctx context.Context, raw string) error {
	sum := sha256.Sum256([]byte(raw))
	_, err := s.db.Exec(ctx, `UPDATE auth_sessions SET revoked_at=NOW() WHERE refresh_token_hash=$1`, fmt.Sprintf("%x", sum[:]))
	return err
}

func (s *Service) RecordSession(ctx context.Context, userID uuid.UUID, refresh string) error {
	sum := sha256.Sum256([]byte(refresh))
	_, err := s.db.Exec(ctx, `INSERT INTO auth_sessions(id,user_id,refresh_token_hash,expires_at) VALUES($1,$2,$3,NOW()+INTERVAL '30 days') ON CONFLICT(refresh_token_hash) DO NOTHING`, uuid.New(), userID, fmt.Sprintf("%x", sum[:]))
	return err
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE users SET deleted_at=NOW(), email=NULL, password_hash=NULL, updated_at=NOW() WHERE id=$1`, id)
	return err
}

func (s *Service) ChangePassword(ctx context.Context, id uuid.UUID, oldPassword, newPassword string) error {
	if len(newPassword) < 8 {
		return ErrInvalidCredentials
	}
	var hash string
	if err := s.db.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&hash); err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(oldPassword)) != nil {
		return ErrInvalidCredentials
	}
	next, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `UPDATE users SET password_hash=$2,updated_at=NOW() WHERE id=$1`, id, string(next))
	return err
}

func (s *Service) Export(ctx context.Context, id uuid.UUID) (User, error) {
	var u User
	err := s.db.QueryRow(ctx, `SELECT id,COALESCE(email,''),COALESCE(display_name,''),role FROM users WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role)
	return u, err
}

func (s *Service) HardDelete(ctx context.Context, id uuid.UUID) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `DELETE FROM users WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
