package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
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
	db  *sql.DB
	rdb *redis.Client
}

func New(ctx context.Context, mysqlDSN, redisAddr string) (*Store, error) {
	db, err := sql.Open("mysql", mysqlDSN)
	if err != nil {
		return nil, fmt.Errorf("sql open mysql: %w", err)
	}
	db.SetMaxOpenConns(15)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("mysql ping: %w", err)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:         redisAddr,
		DialTimeout:  2 * time.Second,
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
	})
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		db.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	return &Store{db: db, rdb: rdb}, nil
}

func (s *Store) Close() {
	if s.rdb != nil {
		_ = s.rdb.Close()
	}
	if s.db != nil {
		_ = s.db.Close()
	}
}

func (s *Store) Ready(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("mysql unreachable: %w", err)
	}
	if err := s.rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis unreachable: %w", err)
	}
	return nil
}

func (s *Store) CreateOrder(ctx context.Context, customerID, itemSKU string, amountCents int64, riskScore float64, riskTier string) (Order, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO orders (id, customer_id, item_sku, amount_cents, risk_score, risk_tier, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, customerID, itemSKU, amountCents, riskScore, riskTier, now)
	if err != nil {
		return Order{}, fmt.Errorf("insert order: %w", err)
	}

	_ = s.rdb.Del(ctx, "analytics:summary").Err()

	return Order{
		ID:          id,
		CustomerID:  customerID,
		ItemSKU:     itemSKU,
		AmountCents: amountCents,
		RiskScore:   riskScore,
		RiskTier:    riskTier,
		CreatedAt:   now,
	}, nil
}

func (s *Store) GetOrderByID(ctx context.Context, id string) (Order, bool, error) {
	cacheKey := "order:" + id
	if cached, err := s.rdb.Get(ctx, cacheKey).Bytes(); err == nil {
		var o Order
		if json.Unmarshal(cached, &o) == nil {
			return o, true, nil
		}
	}

	var o Order
	err := s.db.QueryRowContext(ctx, `
		SELECT id, customer_id, item_sku, amount_cents, risk_score, risk_tier, created_at
		FROM orders
		WHERE id = ?
	`, id).Scan(&o.ID, &o.CustomerID, &o.ItemSKU, &o.AmountCents, &o.RiskScore, &o.RiskTier, &o.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, false, ErrNotFound
	}
	if err != nil {
		return Order{}, false, fmt.Errorf("query order: %w", err)
	}

	if payload, err := json.Marshal(o); err == nil {
		_ = s.rdb.Set(ctx, cacheKey, payload, 60*time.Second).Err()
	}
	return o, false, nil
}
