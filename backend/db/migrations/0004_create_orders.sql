-- Accepted orders, with their current remaining quantity and status.
-- Design: docs/database-schema.md, "orders".
--
-- Rejected submissions are never stored, so REJECTED is not a valid status.
-- order_id, sequence, status, and both timestamps come from the exchange, so
-- no column has a default. quantity never changes after insert; remaining,
-- status, and updated_at do.
CREATE TABLE orders (
	order_id   text COLLATE "C" NOT NULL,
	sequence   bigint           NOT NULL,
	user_id    text COLLATE "C" NOT NULL,
	symbol     text COLLATE "C" NOT NULL,
	side       text COLLATE "C" NOT NULL,
	order_type text COLLATE "C" NOT NULL,
	price      bigint           NOT NULL,
	quantity   bigint           NOT NULL,
	remaining  bigint           NOT NULL,
	status     text COLLATE "C" NOT NULL,
	created_at timestamptz      NOT NULL,
	updated_at timestamptz      NOT NULL,
	CONSTRAINT orders_pkey PRIMARY KEY (order_id),
	CONSTRAINT orders_sequence_key UNIQUE (sequence),
	-- Trivially unique because order_id is the key. It is the target of the
	-- executions' composite foreign keys, which prove that an execution's
	-- buyer, seller, and symbol agree with its orders.
	CONSTRAINT orders_order_id_user_id_symbol_key UNIQUE (order_id, user_id, symbol),
	CONSTRAINT orders_user_id_fkey FOREIGN KEY (user_id) REFERENCES accounts (user_id),
	CONSTRAINT orders_symbol_fkey FOREIGN KEY (symbol) REFERENCES instruments (symbol),
	CONSTRAINT orders_order_id_check CHECK (char_length(order_id) BETWEEN 1 AND 64),
	CONSTRAINT orders_sequence_check CHECK (sequence BETWEEN 1 AND 9007199254740991),
	CONSTRAINT orders_side_check CHECK (side IN ('BUY', 'SELL')),
	CONSTRAINT orders_order_type_check CHECK (order_type IN ('LIMIT', 'MARKET')),
	CONSTRAINT orders_price_check CHECK (
		(order_type = 'LIMIT' AND price BETWEEN 1 AND 100000000)
		OR (order_type = 'MARKET' AND price = 0)
	),
	CONSTRAINT orders_quantity_check CHECK (quantity BETWEEN 1 AND 1000000),
	CONSTRAINT orders_remaining_check CHECK (remaining >= 0 AND remaining <= quantity),
	CONSTRAINT orders_status_check CHECK (status IN ('NEW', 'PARTIALLY_FILLED', 'FILLED', 'CANCELED')),
	-- Order-model invariants 7 to 10.
	CONSTRAINT orders_status_remaining_check CHECK (
		(status = 'NEW' AND remaining = quantity)
		OR (status = 'PARTIALLY_FILLED' AND remaining > 0 AND remaining < quantity)
		OR (status = 'FILLED' AND remaining = 0)
		OR (status = 'CANCELED' AND remaining > 0)
	),
	-- Order-model invariant 11: updated_at moves only on a fill or a
	-- cancellation, and a NEW order has had neither.
	CONSTRAINT orders_updated_at_check CHECK (
		updated_at >= created_at AND (status <> 'NEW' OR updated_at = created_at)
	)
);

-- GET /api/v1/orders?userId=..., newest first. Also covers orders_user_id_fkey.
CREATE INDEX orders_user_id_sequence_idx ON orders (user_id, sequence DESC);

-- GET /api/v1/orderbook, reservation reconciliation, and rebuilding the book.
-- Partial, so it holds only resting orders.
CREATE INDEX orders_open_book_idx ON orders (symbol, side, price) INCLUDE (remaining)
	WHERE status IN ('NEW', 'PARTIALLY_FILLED');
