-- Share balances, one row per account and symbol.
-- Design: docs/database-schema.md, "positions".
--
-- 0 <= reserved <= total <= 2^53 - 1. total >= 0 is the no-short-selling
-- rule. reserved is the shares held for the account's open SELL orders.
CREATE TABLE positions (
	user_id  text COLLATE "C" NOT NULL,
	symbol   text COLLATE "C" NOT NULL,
	total    bigint           NOT NULL DEFAULT 0,
	reserved bigint           NOT NULL DEFAULT 0,
	CONSTRAINT positions_pkey PRIMARY KEY (user_id, symbol),
	CONSTRAINT positions_user_id_fkey FOREIGN KEY (user_id) REFERENCES accounts (user_id),
	CONSTRAINT positions_symbol_fkey FOREIGN KEY (symbol) REFERENCES instruments (symbol),
	CONSTRAINT positions_total_check CHECK (total BETWEEN 0 AND 9007199254740991),
	CONSTRAINT positions_reserved_check CHECK (reserved >= 0 AND reserved <= total)
);
