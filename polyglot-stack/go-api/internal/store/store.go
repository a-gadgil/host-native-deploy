package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

var ErrOrderNotFound = errors.New("order not found")

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
	db  *pgxpool.Pool
	rdb *redis.Client
}

func New(db *pgxpool.Pool, rdb *redis.Client) *Store {
	return &Store{db: db, rdb: rdb}
}

func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.Ping(ctx); err != nil {
		return fmt.Errorf("postgres ping: %w", err)
	}
	if err := s.rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis ping: %w", err)
	}
	return nil
}

func (s *Store) CreateOrder(ctx context.Context, o Order) (Order, error) {
	err := s.db.QueryRow(ctx, `
		INSERT INTO orders (id, customer_id, item_sku, amount_cents, risk_score, risk_tier)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at;
	`, o.ID, o.CustomerID, o.ItemSKU, o.AmountCents, o.RiskScore, o.RiskTier).Scan(&o.CreatedAt)
	if err != nil {
		return Order{}, fmt.Errorf("insert order: %w", err)
	}

	// Best-effort cache invalidation so Python's analytics endpoint recomputes fresh totals
	_ = s.rdb.Del(ctx, "analytics:summary").Err()
	return o, nil
}

func (s *Store) GetOrder(ctx context.Context, id string) (Order, bool, error) {
	cacheKey := "order:" + id

	// 1. Cache-aside lookup in Redis
	if cached, err := s.rdb.Get(ctx, cacheKey).Result(); err == nil {
		var o Order
		if json.Unmarshal([]byte(cached), &o) == nil {
			return o, true, nil
		}
	}

	// 2. Fallback to PostgreSQL
	var o Order
	err := s.db.QueryRow(ctx, `
		SELECT id::text, customer_id, item_sku, amount_cents, risk_score::float8, risk_tier, created_at
		FROM orders
		WHERE id = $1;
	`, id).Scan(&o.ID, &o.CustomerID, &o.ItemSKU, &o.AmountCents, &o.RiskScore, &o.RiskTier, &o.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Order{}, false, ErrOrderNotFound
		}
		return Order{}, false, fmt.Errorf("select order: %w", err)
	}

	// 3. Populate Redis cache with 60s TTL (best-effort)
	if payload, err := json.Marshal(o); err == nil {
		_ = s.rdb.Set(ctx, cacheKey, payload, 60*time.Second).Err()
	}

	return o, false, nil
}

func (s *Store) ListOrders(ctx context.Context) ([]Order, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text, customer_id, item_sku, amount_cents, risk_score::float8, risk_tier, created_at
		FROM orders
		ORDER BY created_at DESC
		LIMIT 20;
	`)
	if err != nil {
		return nil, fmt.Errorf("list orders: %w", err)
	}
	defer rows.Close()

	orders := make([]Order, 0)
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.CustomerID, &o.ItemSKU, &o.AmountCents, &o.RiskScore, &o.RiskTier, &o.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan order: %w", err)
		}
		orders = append(orders, o)
	}
	return orders, rows.Err()
}
