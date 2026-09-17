package db

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

func PurgeExpired(ctx context.Context, pool *pgxpool.Pool, days int) error {
	if days <= 0 {
		return nil
	}
	if _, e := pool.Exec(ctx, `DELETE FROM account_tokens WHERE expires_at<NOW() OR used_at<NOW()-INTERVAL '7 days'`); e != nil {
		return e
	}
	if _, e := pool.Exec(ctx, `DELETE FROM auth_sessions WHERE expires_at<NOW() OR revoked_at<NOW()-INTERVAL '30 days'`); e != nil {
		return e
	}
	if _, e := pool.Exec(ctx, `DELETE FROM audit_logs WHERE created_at<NOW()-$1::int*INTERVAL '1 day'`, days); e != nil {
		return e
	}
	_, e := pool.Exec(ctx, `DELETE FROM ai_safety_events WHERE created_at<NOW()-$1::int*INTERVAL '1 day' AND status<>'open'`, days)
	return e
}
func StartMaintenance(ctx context.Context, pool *pgxpool.Pool, days int) {
	go func() {
		t := time.NewTicker(6 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				PurgeExpired(ctx, pool, days)
			}
		}
	}()
}
