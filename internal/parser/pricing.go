package parser

import "strings"

// Pricing holds per-million-token rates for a single model.
type Pricing struct {
	Input       float64
	Output      float64
	CacheRead   float64
	CacheCreate float64
}

// pricingTable mirrors src/pricing.py _DEFAULT_PRICING exactly.
// The order matters for substring matching -- more specific entries should
// appear before generic ones so that exact matches win first.
var pricingTable = []struct {
	Key     string
	Pricing Pricing
}{
	{"claude-opus-4-6", Pricing{Input: 15.0, Output: 75.0, CacheRead: 1.875, CacheCreate: 18.75}},
	{"claude-opus-4-5-20251101", Pricing{Input: 15.0, Output: 75.0, CacheRead: 1.875, CacheCreate: 18.75}},
	{"claude-sonnet-4-6", Pricing{Input: 3.0, Output: 15.0, CacheRead: 0.375, CacheCreate: 3.75}},
	{"claude-sonnet-4-5-20241022", Pricing{Input: 3.0, Output: 15.0, CacheRead: 0.375, CacheCreate: 3.75}},
	{"claude-haiku-4-5-20251001", Pricing{Input: 0.80, Output: 4.0, CacheRead: 0.08, CacheCreate: 1.0}},
}

// opusFallback is used when no pricing entry matches the model string.
var opusFallback = Pricing{Input: 15.0, Output: 75.0, CacheRead: 1.875, CacheCreate: 18.75}

// GetPricing returns the pricing for a model using substring matching,
// replicating the behavior of get_pricing() in src/pricing.py.
// Unknown models fall back to Opus pricing.
func GetPricing(model string) Pricing {
	for _, entry := range pricingTable {
		if strings.Contains(model, entry.Key) {
			return entry.Pricing
		}
	}
	return opusFallback
}

// CalculateCost computes the estimated cost in USD for a single record.
// Formula: sum of (tokens * rate / 1,000,000) across all four token categories.
func CalculateCost(p Pricing, inputTokens, outputTokens, cacheRead, cacheCreate int) float64 {
	return float64(inputTokens)*p.Input/1_000_000 +
		float64(outputTokens)*p.Output/1_000_000 +
		float64(cacheRead)*p.CacheRead/1_000_000 +
		float64(cacheCreate)*p.CacheCreate/1_000_000
}
