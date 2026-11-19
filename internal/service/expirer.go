package service

import (
	"context"
	"time"
)

func RunHoldExpirer(ctx context.Context, svc *BookingService, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			svc.ReleaseExpired(ctx)
		}
	}
}
