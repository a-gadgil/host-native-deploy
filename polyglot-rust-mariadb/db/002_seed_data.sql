INSERT INTO orders (id, customer_id, item_sku, amount_cents, risk_score, risk_tier, created_at)
SELECT '11111111-1111-4111-8111-111111111101', 'cust-101', 'SKU-RUST-CORE', 49900, 12.50, 'LOW', UTC_TIMESTAMP(6)
WHERE NOT EXISTS (SELECT 1 FROM orders WHERE id = '11111111-1111-4111-8111-111111111101');

INSERT INTO orders (id, customer_id, item_sku, amount_cents, risk_score, risk_tier, created_at)
SELECT '11111111-1111-4111-8111-111111111102', 'cust-101', 'SKU-MARIADB-HA', 185000, 38.00, 'MEDIUM', UTC_TIMESTAMP(6)
WHERE NOT EXISTS (SELECT 1 FROM orders WHERE id = '11111111-1111-4111-8111-111111111102');

INSERT INTO orders (id, customer_id, item_sku, amount_cents, risk_score, risk_tier, created_at)
SELECT '11111111-1111-4111-8111-111111111103', 'cust-202', 'SKU-EDGE-MESH', 320000, 52.00, 'MEDIUM', UTC_TIMESTAMP(6)
WHERE NOT EXISTS (SELECT 1 FROM orders WHERE id = '11111111-1111-4111-8111-111111111103');
