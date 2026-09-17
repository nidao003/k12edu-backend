package cache

import (
	"context"
	"fmt"
	"github.com/redis/go-redis/v9"
)

func Open(ctx context.Context, rawURL string) (*redis.Client, error) {
	if rawURL == "" {
		return nil, nil
	}
	opt, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	client := redis.NewClient(opt)
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return client, nil
}
