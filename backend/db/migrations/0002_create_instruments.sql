-- Tradable symbols.
-- Design: docs/database-schema.md, "instruments".
--
-- Symbols are exact uppercase strings; the server never normalizes case, so
-- 'aapl' is not a symbol and cannot be referenced by positions or orders.
CREATE TABLE instruments (
	symbol text COLLATE "C" NOT NULL,
	name   text             NOT NULL,
	CONSTRAINT instruments_pkey PRIMARY KEY (symbol),
	CONSTRAINT instruments_symbol_check CHECK (symbol ~ '^[A-Z]{1,16}$'),
	CONSTRAINT instruments_name_check CHECK (char_length(name) BETWEEN 1 AND 200)
);
