package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Aotorun/ticketflow/internal/config"
	"github.com/Aotorun/ticketflow/internal/postgres"
	"github.com/Aotorun/ticketflow/internal/redisclient"
	"github.com/Aotorun/ticketflow/internal/service"
)

func main() {
	cfg := config.Load()

	ctx := context.Background()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("postgres connect: %v", err)
	}
	defer pool.Close()
	fmt.Println("postgres connected")

	repo := postgres.NewBookingPool(pool)

	rdb, err := redisclient.NewClient(ctx, cfg.RedisAddr)
	if err != nil {
		log.Fatalf("redis connect: %v", err)
	}
	defer rdb.Close()
	fmt.Println("redis connected")

	svc := service.NewBookingService(pool, rdb)

	venueID, err := repo.CreateVenue(ctx, "Test Arena")
	if err != nil {
		log.Fatalf("create venue: %v", err)
	}
	fmt.Println("venue created:", venueID)

	if err := repo.AddSeats(ctx, venueID, 3, 5); err != nil {
		log.Fatalf("add seats: %v", err)
	}
	fmt.Println("seats added")

	eventID, err := svc.CreateEvent(ctx, venueID, "Test Concert", time.Now().Add(24*time.Hour))
	if err != nil {
		log.Fatalf("create event: %v", err)
	}
	fmt.Println("event created:", eventID)

	start := time.Now()
	data1, err := svc.GetSeatMap(ctx, eventID)
	if err != nil {
		log.Fatalf("get seat map (1st, expect miss): %v", err)
	}
	fmt.Println("1st call took:", time.Since(start))
	fmt.Println("data:", data1)

	start = time.Now()
	data2, err := svc.GetSeatMap(ctx, eventID)
	if err != nil {
		log.Fatalf("get seat map (2nd, expect hit): %v", err)
	}
	fmt.Println("2nd call took:", time.Since(start))

	fmt.Println("data matches:", data1 == data2)
}
