package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Aotorun/ticketflow/internal/config"
	"github.com/Aotorun/ticketflow/internal/handler"
	"github.com/Aotorun/ticketflow/internal/postgres"
	"github.com/Aotorun/ticketflow/internal/redisclient"
	"github.com/Aotorun/ticketflow/internal/service"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg := config.Load()

	r := gin.Default()
	if err := r.SetTrustedProxies(nil); err != nil {
		log.Fatalf("trusted proxies: %v", err)
	}

	r.GET("/health", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("postgres connect: %v", err)
	}
	defer pool.Close()
	fmt.Println("postgres connected")

	rdb, err := redisclient.NewClient(ctx, cfg.RedisAddr)
	if err != nil {
		log.Fatalf("redis connect: %v", err)
	}
	defer rdb.Close()
	fmt.Println("redis connected")

	svc := service.NewBookingService(pool, rdb)
	h := handler.NewBookingHandler(svc)

	wg := sync.WaitGroup{}

	ctxExpire, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	wg.Add(1)
	go func() {
		service.RunHoldExpirer(ctxExpire, svc, 10*time.Second)
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		service.RunReconciler(ctxExpire, svc, time.Minute)
		wg.Done()
	}()

	r.GET("/events/:id/seats", h.GetSeatMap)
	r.POST("/events/:id/reserve", h.ReserveAny)
	r.POST("/events/:id/seats/:seat_id/reserve", h.ReserveSpecific)
	r.POST("/events/:id/seats/:seat_id/confirm", h.Confirm)

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: r}
	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Print(err)
		}
		errCh <- err
	}()

	select {
	case <-errCh:
	case <-ctxExpire.Done():
	}

	stop()

	log.Print("shutting down")
	ctxShutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctxShutdown); err != nil {
		log.Printf("shutdown: %v", err)
	}
	wg.Wait()
}
