package auth

import (
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestAccessAndRefreshTokenTypes(t *testing.T) {
	s := NewService(nil, "test-secret")
	u := User{ID: uuid.New(), Email: "test@example.com", Role: "student"}
	access, refresh, err := s.tokens(u)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Parse(access); err != nil {
		t.Fatalf("access token rejected: %v", err)
	}
	if _, _, err = s.Parse(refresh); err == nil {
		t.Fatal("refresh token accepted as access token")
	}
}

func TestTokenExpires(t *testing.T) {
	s := NewService(nil, "test-secret")
	s.accessTTL = time.Millisecond
	u := User{ID: uuid.New(), Role: "student"}
	token, _, err := s.tokens(u)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, _, err = s.Parse(token); err == nil {
		t.Fatal("expired token accepted")
	}
}
