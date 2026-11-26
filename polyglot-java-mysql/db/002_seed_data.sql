INSERT IGNORE INTO orders (id, customer_id, item_sku, amount_cents, risk_score, risk_tier)
VALUES
    ('22222222-2222-2222-2222-222222222201', 'cust-101', 'SKU-LAPTOP-01', 89900, 12.50, 'LOW'),
    ('22222222-2222-2222-2222-222222222202', 'cust-102', 'SKU-SERVER-04', 450000, 58.00, 'MEDIUM');
