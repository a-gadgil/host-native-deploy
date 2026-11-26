CREATE TABLE IF NOT EXISTS orders (
    id CHAR(36) PRIMARY KEY,
    customer_id VARCHAR(64) NOT NULL,
    item_sku VARCHAR(64) NOT NULL,
    amount_cents BIGINT NOT NULL CHECK (amount_cents > 0),
    risk_score DECIMAL(5, 2) NOT NULL DEFAULT 0.00,
    risk_tier VARCHAR(16) NOT NULL DEFAULT 'LOW',
    created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    INDEX idx_orders_customer_created (customer_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
