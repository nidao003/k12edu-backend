package storage

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"io"
	"os"
	"path/filepath"
)

type Store interface {
	Put(context.Context, string, io.Reader) (string, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}
type Local struct{ Root string }

func NewLocal(root string) (*Local, error) {
	if root == "" {
		return nil, fmt.Errorf("storage root is required")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	return &Local{Root: root}, nil
}
func (s *Local) Put(_ context.Context, name string, r io.Reader) (string, error) {
	key := filepath.Join(uuid.NewString(), filepath.Base(name))
	path := filepath.Join(s.Root, key)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err = io.Copy(f, r); err != nil {
		return "", err
	}
	return key, nil
}
func (s *Local) Open(_ context.Context, key string) (io.ReadCloser, error) {
	p := filepath.Join(s.Root, key)
	if filepath.Clean(p) != filepath.Join(s.Root, filepath.Clean(key)) {
		return nil, fmt.Errorf("invalid storage key")
	}
	return os.Open(p)
}
func (s *Local) Delete(_ context.Context, key string) error {
	return os.Remove(filepath.Join(s.Root, key))
}
