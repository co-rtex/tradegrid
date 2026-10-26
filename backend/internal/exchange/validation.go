package exchange

// Submission bounds from docs/api.md. Together they cap price * quantity at
// 100 trillion, safely below both int64 and JavaScript's safe-integer limit.
const (
	MaxPrice    int64 = 100_000_000
	MaxQuantity int64 = 1_000_000
)
