package config

import "os"

type Config struct {
	AppHost    string
	AppPort    string
	AppEnv     string
	MySQLDSN   string
	RedisAddr  string
	JavaAPIURL string
}

func Load() Config {
	return Config{
		AppHost:    getEnv("APP_HOST", "127.0.0.1"),
		AppPort:    getEnv("APP_PORT", "8090"),
		AppEnv:     getEnv("APP_ENV", "host-native"),
		MySQLDSN:   getEnv("MYSQL_DSN", "polyglot_app:polyglot_dev_pass@tcp(127.0.0.1:3306)/polyglot_mysql_db?parseTime=true&loc=UTC&timeout=2s"),
		RedisAddr:  getEnv("REDIS_ADDR", "127.0.0.1:6379"),
		JavaAPIURL: getEnv("JAVA_API_URL", "http://127.0.0.1:8091"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
