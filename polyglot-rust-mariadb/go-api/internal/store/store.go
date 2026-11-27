package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

var ErrNotFound = errors.New("order not found")

type Order struct {
	ID          string    `json:"id"`
	CustomerID  string    `json:"customer_id"`
	ItemSKU     string    `json:"item_sku"`
	AmountCents int64     `json:"amount_cents"`
	RiskScore   float64   `json:"risk_score"`
	RiskTier    string    `json:"risk_tier"`
	CreatedAt   time.Time `json:"created_at"`
}

type Store struct {
	db    *sql.DB
	redis *redis.Client
}

func New(ctx context.Context, dsn, redisAddr string) (*Store, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mariadb: %w", err)
	}
	db.SetMaxOpenConns(15)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping mariadb: %w", err)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:         redisAddr,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = db.Close()
		_ = rdb.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return &Store{db: db, redis: rdb}, nil
}

func (s *Store) Close() {
	if s.db != nil {
		_ = s.db.Close()
	}
	if s.redis != nil {
		_ = s.redis.Close()
	}
}

func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("mariadb: %w", err)
	}
	if err := s.redis.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	return nil
}

func (s *Store) CreateOrder(ctx context.Context, customerID, itemSKU string, amountCents int64, riskScore float64, riskTier string) (Order, error) {
	const query = `
		INSERT INTO orders (customer_id, item_sku, amount_cents, risk_score, risk_tier)
		VALUES (?, ?, ?, ?, ?)
		RETURNING id, created_at`

	var o Order
	o.CustomerID = customerID
	o.ItemSKU = itemSKU
	o.AmountCents = amountCents
	o.RiskScore = riskScore
	o.RiskTier = riskTier

	if err := s.db.QueryRowContext(ctx, query, customerID, itemSKU, amountCents, riskScore, riskTier).Scan(&o.ID, &o.CreatedAt); err != nil {
		return Order{}, fmt.Errorf("insert order returning: %w", err)
	}
	o.CreatedAt = o.CreatedAt.UTC()

	if payload, err := json.Marshal(o); err == nil {
		_ = s.redis.Set(ctx, "order:"+o.ID, payload, 5*time.Minute).Err()
	}
	_ = s.redis.Del(ctx, "analytics:summary").Err()

	return o, nil
}

func (s *Store) GetOrder(ctx context.Context, id string) (Order, string, error) {
	if cached, err := s.redis.Get(ctx, "order:"+id).Result(); err == nil {
		var o Order
		if json.Unmarshal([]byte(cached), &o) == nil {
			return o, "HIT", nil
		}
	}

	const query = `
		SELECT id, customer_id, item_sku, amount_cents, risk_score, risk_tier, created_at
		FROM orders
		WHERE id = ?`

	var o Order
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&o.ID, &o.CustomerID, &o.ItemSKU, &o.AmountCents, &o.RiskScore, &o.RiskTier, &o.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, "MISS", ErrNotFound
	}
	if err != nil {
		return Order{}, "MISS", fmt.Errorf("select order: %w", err)
	}
	o.CreatedAt = o.CreatedAt.UTC()

	if payload, err := json.Marshal(o); err == nil {
		_ = s.redis.Set(ctx, "order:"+o.ID, payload, 5*time.Minute).Err()
	}
	return o, "MISS", nil
}
