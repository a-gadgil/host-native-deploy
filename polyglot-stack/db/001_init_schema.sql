CREATE TABLE IF NOT EXISTS orders (
    id UUID PRIMARY KEY,
    customer_id VARCHAR(64) NOT NULL,
    item_sku VARCHAR(64) NOT NULL,
    amount_cents BIGINT NOT NULL CHECK (amount_cents > 0),
    risk_score NUMERIC(4, 2) NOT NULL DEFAULT 0.00,
    risk_tier VARCHAR(16) NOT NULL DEFAULT 'LOW',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_orders_customer_created
    ON orders (customer_id, created_at DESC);

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE orders TO polyglot_app;
