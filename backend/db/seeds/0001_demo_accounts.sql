-- Demo accounts and symbols from docs/api.md, "Demo Accounts and Symbols",
-- matching backend/internal/accounts/seed.go and docs/fixtures/00-seed.json.
-- No orders and no executions.
--
-- Safe to rerun: every row is inserted with ON CONFLICT (primary key) DO
-- NOTHING, so a rerun adds no rows, never adds to a balance, and never resets
-- a balance that trading has since changed. To start over, use a new database
-- (see backend/db/README.md).

INSERT INTO instruments (symbol, name) VALUES
	('AAPL', 'Apple Inc.'),
	('MSFT', 'Microsoft Corporation')
ON CONFLICT (symbol) DO NOTHING;

-- 10000000 cents is $100,000.00.
INSERT INTO accounts (user_id, display_name, cash_total, cash_reserved) VALUES
	('alice', 'Alice (demo)', 10000000, 0),
	('bob', 'Bob (demo)', 10000000, 0)
ON CONFLICT (user_id) DO NOTHING;

INSERT INTO positions (user_id, symbol, total, reserved) VALUES
	('alice', 'AAPL', 100, 0),
	('alice', 'MSFT', 100, 0),
	('bob', 'AAPL', 100, 0),
	('bob', 'MSFT', 100, 0)
ON CONFLICT (user_id, symbol) DO NOTHING;
