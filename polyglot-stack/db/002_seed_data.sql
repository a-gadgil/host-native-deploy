INSERT INTO orders (id, customer_id, item_sku, amount_cents, risk_score, risk_tier)
VALUES
    ('11111111-1111-1111-1111-111111111101', 'cust-101', 'SKU-LAPTOP-01', 89900, 12.50, 'LOW'),
    ('11111111-1111-1111-1111-111111111102', 'cust-102', 'SKU-SERVER-04', 450000, 58.00, 'MEDIUM')
ON CONFLICT (id) DO NOTHING;
