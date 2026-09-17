package storage

import (
	"context"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"io"
	"os"
	"strings"
)

type S3 struct {
	client         *minio.Client
	bucket, prefix string
}

func NewS3(endpoint, accessKey, secretKey, bucket, region, prefix string, useSSL bool) (*S3, error) {
	host := strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://")
	c, e := minio.New(host, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: useSSL, Region: region})
	if e != nil {
		return nil, e
	}
	return &S3{client: c, bucket: bucket, prefix: strings.Trim(prefix, "/")}, nil
}
func (s *S3) key(k string) string {
	if s.prefix == "" {
		return k
	}
	return s.prefix + "/" + k
}
func (s *S3) Put(ctx context.Context, name string, r io.Reader) (string, error) {
	key := s.key(uuid.NewString() + "/" + name)
	_, e := s.client.PutObject(ctx, s.bucket, key, r, -1, minio.PutObjectOptions{})
	return key, e
}
func (s *S3) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{}), nil
}
func (s *S3) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}
func NewConfigured() (Store, error) {
	if os.Getenv("K12EDU_STORAGE_DRIVER") == "s3" {
		return NewS3(os.Getenv("K12EDU_STORAGE_ENDPOINT"), os.Getenv("K12EDU_STORAGE_ACCESS_KEY"), os.Getenv("K12EDU_STORAGE_SECRET_KEY"), os.Getenv("K12EDU_STORAGE_BUCKET"), os.Getenv("K12EDU_STORAGE_REGION"), "", os.Getenv("K12EDU_STORAGE_SSL") == "true")
	}
	return NewLocal("./data/files")
}
