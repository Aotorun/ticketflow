package redisclient

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

func NewClient(ctx context.Context, addr string) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{Addr: addr})
	err := client.Ping(ctx).Err()
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("ping error: %w", err)
	}

	return client, nil
}
