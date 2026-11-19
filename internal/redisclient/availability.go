package redisclient

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type ReserveResult int

const (
	Reserved ReserveResult = 1
	SoldOut  ReserveResult = 0
	Unknown  ReserveResult = -1
)

const luaTryReserve = `
local count = tonumber(redis.call('GET', KEYS[1]))
if count == nil then
  return -1
end
if count <= 0 then
  return 0
end		
redis.call('DECR', KEYS[1])
return 1
`

const luaRelease = `
if redis.call('EXISTS', KEYS[1]) == 0 then
  return -1
end
return redis.call('INCRBY', KEYS[1], ARGV[1])
`

type AvailabilityCounter struct {
	client *redis.Client
}

func NewAvailabilityCounter(client *redis.Client) *AvailabilityCounter {
	return &AvailabilityCounter{client}
}

func (c *AvailabilityCounter) InitCount(ctx context.Context, eventID uuid.UUID, count int) error {
	key := fmt.Sprintf("available:%s", eventID)
	err := c.client.Set(ctx, key, count, 0).Err()
	if err != nil {
		return fmt.Errorf("init counter: %w", err)
	}

	return nil
}

func (c *AvailabilityCounter) TryReserve(ctx context.Context, eventID uuid.UUID) (ReserveResult, error) {
	key := fmt.Sprintf("available:%s", eventID)
	res, err := c.client.Eval(ctx, luaTryReserve, []string{key}).Result()
	if err != nil {
		return Unknown, fmt.Errorf("lua script: %w", err)
	}

	n, ok := res.(int64)
	if !ok {
		return Unknown, fmt.Errorf("unexpected lua result type: %T", res)
	}

	resRes := ReserveResult(n)
	if resRes != Reserved && resRes != SoldOut && resRes != Unknown {
		return Unknown, fmt.Errorf("unknown result: %v", resRes)
	}

	return resRes, nil
}

func (c *AvailabilityCounter) Release(ctx context.Context, eventID uuid.UUID, n int) error {
	if n <= 0 {
		return nil
	}

	key := fmt.Sprintf("available:%s", eventID)
	if err := c.client.Eval(ctx, luaRelease, []string{key}, n).Err(); err != nil {
		return fmt.Errorf("lua release: %w", err)
	}

	return nil
}

func (c *AvailabilityCounter) InitIfAbsent(ctx context.Context, eventID uuid.UUID, count int) (bool, error) {
	key := fmt.Sprintf("available:%s", eventID)
	ok, err := c.client.SetNX(ctx, key, count, 0).Result()
	if err != nil {
		return false, fmt.Errorf("init counter if absent: %w", err)
	}

	return ok, nil
}

func (c *AvailabilityCounter) Take(ctx context.Context, eventID uuid.UUID) error {
	_, err := c.TryReserve(ctx, eventID)
	return err
}

func (c *AvailabilityCounter) Reset(ctx context.Context, eventID uuid.UUID, count int) (int, bool, error) {
	key := fmt.Sprintf("available:%s", eventID)
	res, err := c.client.SetArgs(ctx, key, count, redis.SetArgs{Get: true}).Result()
	if errors.Is(err, redis.Nil) {
		return 0, false, nil
	}

	if err != nil {
		return 0, false, fmt.Errorf("reset counter: %w", err)
	}

	old, err := strconv.Atoi(res)
	if err != nil {
		return 0, true, fmt.Errorf("parse old counter %q: %w", res, err)
	}

	return old, true, nil
}
