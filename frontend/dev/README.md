# Market data fixture preview

From `frontend/`, use Node 24 (or another version supported by package.json),
run `npm ci` if dependencies are missing, then `npm run dev -- --host 127.0.0.1`.
Open the URL Vite prints, followed by:

- `/dev/market-data.html?scenario=partial-fill`: populated bid and execution.
- `/dev/market-data.html?scenario=seed`: empty book and recent trades.
- `/dev/market-data.html?scenario=no-fill`: ask-only book.
- `/dev/market-data.html?scenario=full-fill`: empty book and one execution.
- `/dev/market-data.html?scenario=rejection`: empty book and trades after rejection.

The scenario links switch between these real `docs/fixtures` files. No backend
is needed. The preview is a separate development entry; production App.tsx,
Vite build inputs, and package scripts are unchanged. The development guard
prevents loading the preview outside Vite development mode.

For screenshots, open partial-fill and seed at a desktop viewport of 1280×900
and capture each page. At 390×844, check that book sides stack and wide tables
scroll horizontally. Keep both BUY / BID and SELL / ASK labels visible in the
empty screenshot; capture the execution row and 15 remaining shares in the
populated screenshot. Browser screenshots must be taken from the rendered
preview, not generated mockups.

State precedence in both components is error, then loading, then supplied data.
An empty error string is treated as no error. Missing order-book data is distinct
from a valid empty snapshot. Invalid numeric/time display values use an em dash;
valid zero quantities are displayed without removing supplied rows.
