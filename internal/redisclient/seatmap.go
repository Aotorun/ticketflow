package redisclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Aotorun/ticketflow/internal/domain"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type SeatMapCache struct {
	client *redis.Client
}

func NewSeatMapCache(client *redis.Client) *SeatMapCache {
	return &SeatMapCache{client}
}

type cachedSeat struct {
	EventID   uuid.UUID  `json:"event_id"`
	SeatID    uuid.UUID  `json:"seat_id"`
	Status    string     `json:"status"`
	HeldBy    *uuid.UUID `json:"held_by,omitempty"`
	HeldUntil *time.Time `json:"held_until,omitempty"`
}

func (m *SeatMapCache) Get(ctx context.Context, eventID uuid.UUID) ([]domain.EventSeat, bool, error) {
	key := fmt.Sprintf("seatmap:%s", eventID)
	val, err := m.client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("get value for key %q: %w", key, err)
	}

	var items []cachedSeat
	if err := json.Unmarshal([]byte(val), &items); err != nil {
		return nil, false, fmt.Errorf("unmarshal seats: %w", err)
	}

	seats := make([]domain.EventSeat, 0, len(items))
	for _, it := range items {
		seats = append(seats, domain.EventSeat{
			EventID:   it.EventID,
			SeatID:    it.SeatID,
			Status:    domain.SeatStatus(it.Status),
			HeldBy:    it.HeldBy,
			HeldUntil: it.HeldUntil,
		})
	}

	return seats, true, nil
}

func (m *SeatMapCache) Set(ctx context.Context, eventID uuid.UUID, seats []domain.EventSeat, ttl time.Duration) error {
	items := make([]cachedSeat, 0, len(seats))
	for _, s := range seats {
		items = append(items, cachedSeat{
			EventID:   s.EventID,
			SeatID:    s.SeatID,
			Status:    string(s.Status),
			HeldBy:    s.HeldBy,
			HeldUntil: s.HeldUntil,
		})
	}

	data, err := json.Marshal(items)
	if err != nil {
		return fmt.Errorf("marshal seats: %w", err)
	}

	key := fmt.Sprintf("seatmap:%s", eventID)
	if err := m.client.Set(ctx, key, data, ttl).Err(); err != nil {
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
