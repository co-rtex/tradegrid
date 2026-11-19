-- Demo accounts and their cash balances.
-- Design: docs/database-schema.md, "accounts".
--
-- Cash is whole US cents. 0 <= cash_reserved <= cash_total <= 2^53 - 1, which
-- is risk.Balance.valid(); available cash is derived as cash_total - cash_reserved.
CREATE TABLE accounts (
	user_id       text COLLATE "C" NOT NULL,
	display_name  text             NOT NULL,
	cash_total    bigint           NOT NULL,
	cash_reserved bigint           NOT NULL DEFAULT 0,
	CONSTRAINT accounts_pkey PRIMARY KEY (user_id),
	CONSTRAINT accounts_user_id_check CHECK (char_length(user_id) BETWEEN 1 AND 64),
	CONSTRAINT accounts_display_name_check CHECK (char_length(display_name) BETWEEN 1 AND 100),
	CONSTRAINT accounts_cash_total_check CHECK (cash_total BETWEEN 0 AND 9007199254740991),
	CONSTRAINT accounts_cash_reserved_check CHECK (cash_reserved >= 0 AND cash_reserved <= cash_total)
);
