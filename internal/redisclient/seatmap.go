package redisclient

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type SeatMapCache struct {
	client *redis.Client
}

func NewSeatMapCache(client *redis.Client) *SeatMapCache {
	return &SeatMapCache{client}
}

func (m *SeatMapCache) Get(ctx context.Context, eventID uuid.UUID) (string, bool, error) {
	key := fmt.Sprintf("seatmap:%s", eventID)
	val, err := m.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return "", false, nil
		}

		return "", false, fmt.Errorf("get value for key\"%s\": %w", key, err)
	}

	return val, true, nil
}

func (m *SeatMapCache) Set(ctx context.Context, eventID uuid.UUID, data string, ttl time.Duration) error {
	key := fmt.Sprintf("seatmap:%s", eventID)
	err := m.client.Set(ctx, key, data, ttl).Err()
	if err != nil {
		return fmt.Errorf("cannot set value: %w", err)
	}

	return nil
}

func (m *SeatMapCache) Invalidate(ctx context.Context, eventID uuid.UUID) error {
	key := fmt.Sprintf("seatmap:%s", eventID)
	err := m.client.Del(ctx, key).Err()
	if err != nil {
		return fmt.Errorf("cannot delete key \"%s\": %w", key, err)
	}

	return nil
}
