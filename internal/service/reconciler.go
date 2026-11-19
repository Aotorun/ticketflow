package service

import (
	"context"
	"time"
)

func RunReconciler(ctx context.Context, svc *BookingService, interval time.Duration) {
	svc.Reconcile(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			svc.Reconcile(ctx)
		}
	}
}
