package config

import "os"

type Config struct {
	DatabaseURL string
	RedisAddr   string
	HTTPAddr    string
}

func Load() Config {
	return Config{
		DatabaseURL: get("DATABASE_URL", "postgres://ticketflow:ticketflow@localhost:5432/ticketflow"),
		RedisAddr:   get("REDIS_ADDR", "localhost:6379"),
		HTTPAddr:    get("HTTP_ADDR", ":8080"),
	}
}

func get(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}
