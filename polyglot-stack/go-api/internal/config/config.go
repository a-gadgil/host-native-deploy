package config

import "os"

type Config struct {
	AppHost     string
	AppPort     string
	AppEnv      string
	DatabaseURL string
	RedisAddr   string
	PythonURL   string
}

func Load() Config {
	return Config{
		AppHost:     getEnv("APP_HOST", "127.0.0.1"),
		AppPort:     getEnv("APP_PORT", "8090"),
		AppEnv:      getEnv("APP_ENV", "host-native"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://polyglot_app:polyglot_dev_pass@127.0.0.1:5432/polyglot_db?sslmode=disable"),
		RedisAddr:   getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		PythonURL:   getEnv("PYTHON_API_URL", "http://127.0.0.1:8091"),
	}
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return fallback
}
