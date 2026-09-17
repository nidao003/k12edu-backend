package auth

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
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
	return &Service{db: pool, secret: []byte(secret), accessTTL: 30 * time.Minute, refreshTTL: 30 * 24 * time.Hour, appleClientID: id}
}

func (s *Service) AppleLogin(ctx context.Context, identityToken, name string) (User, string, string, error) {
	if s.appleClientID == "" {
		return User{}, "", "", ErrInvalidCredentials
	}
	parsed, err := jwt.Parse(identityToken, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != "RS256" {
			return nil, errors.New("invalid apple algorithm")
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
		for _, k := range keys.Keys {
			if k.Kid == t.Header["kid"] && len(k.X5c) > 0 {
				b, e := base64.StdEncoding.DecodeString(k.X5c[0])
				if e != nil {
					return nil, e
				}
				cert, e := x509.ParseCertificate(b)
				if e != nil {
					return nil, e
				}
				if key, ok := cert.PublicKey.(*rsa.PublicKey); ok {
					return key, nil
				}
			}
		}
		return nil, fmt.Errorf("apple key not found")
	})
	if err != nil || !parsed.Valid {
		return User{}, "", "", ErrInvalidCredentials
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok || claims["iss"] != "https://appleid.apple.com" || claims["aud"] != s.appleClientID {
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
func (s *Service) Login(ctx context.Context, email, password string) (User, string, string, error) {
	var u User
	var hash string
	err := s.db.QueryRow(ctx, `SELECT id,email,display_name,role,password_hash FROM users WHERE email=$1 AND deleted_at IS NULL`, strings.ToLower(strings.TrimSpace(email))).Scan(&u.ID, &u.Email, &u.DisplayName, &u.Role, &hash)
	if errors.Is(err, pgx.ErrNoRows) || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return User{}, "", "", ErrInvalidCredentials
	}
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

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `UPDATE users SET deleted_at=NOW(), email=NULL, password_hash=NULL, updated_at=NOW() WHERE id=$1`, id)
	return err
}
