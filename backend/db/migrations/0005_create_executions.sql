-- Trades between one BUY order and one SELL order. Append-only history.
-- Design: docs/database-schema.md, "executions".
--
-- Both counterparties are stored by order and by user. The composite foreign
-- keys reject an execution whose buyer or seller does not own the named order,
-- or whose symbol differs from either order's. Same-account trades
-- (buyer_user_id = seller_user_id) are allowed. sequence orders the history;
-- executed_at is never used for ordering because ties are expected.
CREATE TABLE executions (
	execution_id   text COLLATE "C" NOT NULL,
	sequence       bigint           NOT NULL,
	executed_at    timestamptz      NOT NULL,
	symbol         text COLLATE "C" NOT NULL,
	price          bigint           NOT NULL,
	quantity       bigint           NOT NULL,
	buy_order_id   text COLLATE "C" NOT NULL,
	sell_order_id  text COLLATE "C" NOT NULL,
	buyer_user_id  text COLLATE "C" NOT NULL,
	seller_user_id text COLLATE "C" NOT NULL,
	CONSTRAINT executions_pkey PRIMARY KEY (execution_id),
	CONSTRAINT executions_sequence_key UNIQUE (sequence),
	CONSTRAINT executions_buy_order_fkey FOREIGN KEY (buy_order_id, buyer_user_id, symbol)
		REFERENCES orders (order_id, user_id, symbol),
	CONSTRAINT executions_sell_order_fkey FOREIGN KEY (sell_order_id, seller_user_id, symbol)
		REFERENCES orders (order_id, user_id, symbol),
	CONSTRAINT executions_execution_id_check CHECK (char_length(execution_id) BETWEEN 1 AND 64),
	CONSTRAINT executions_sequence_check CHECK (sequence BETWEEN 1 AND 9007199254740991),
	CONSTRAINT executions_price_check CHECK (price BETWEEN 1 AND 100000000),
	CONSTRAINT executions_quantity_check CHECK (quantity BETWEEN 1 AND 1000000),
	CONSTRAINT executions_distinct_orders_check CHECK (buy_order_id <> sell_order_id)
);

-- GET /api/v1/executions?symbol=..., newest first.
CREATE INDEX executions_symbol_sequence_idx ON executions (symbol, sequence DESC);

-- GET /api/v1/executions?symbol=...&userId=..., one index per counterparty.
CREATE INDEX executions_buyer_symbol_sequence_idx ON executions (buyer_user_id, symbol, sequence DESC);
CREATE INDEX executions_seller_symbol_sequence_idx ON executions (seller_user_id, symbol, sequence DESC);

-- Fills of one order; these also cover the two composite foreign keys.
CREATE INDEX executions_buy_order_id_idx ON executions (buy_order_id);
CREATE INDEX executions_sell_order_id_idx ON executions (sell_order_id);
