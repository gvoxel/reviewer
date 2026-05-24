package ctl

// modelPrice is the per-1M-token list price for a single model.
// CachedInputPer1M applies to tokens read from the provider's prompt cache.
// When 0, callers should treat cached tokens at the same rate as fresh input.
type modelPrice struct {
	InputPer1M       float64
	OutputPer1M      float64
	CachedInputPer1M float64
}

// modelPrices is the lookup table consulted by CostUsd / CostUsdWithCache.
//
// Numbers are illustrative — verify against the provider's published rates
// before relying on this for billing reconciliation. Anthropic returns exact
// per-run cost via its CLI, so its entries are mostly useful for validation.
// Cache discounts roughly follow the published "10% of base" pattern.
var modelPrices = map[string]modelPrice{
	// Anthropic — Claude 4.x family. Cached read is ~10% of base input.
	"opus":              {InputPer1M: 5, OutputPer1M: 25, CachedInputPer1M: 0.5},
	"claude-opus-4-7":   {InputPer1M: 5, OutputPer1M: 25, CachedInputPer1M: 0.5},
	"claude-opus-4-6":   {InputPer1M: 5, OutputPer1M: 25, CachedInputPer1M: 0.5},
	"sonnet":            {InputPer1M: 3, OutputPer1M: 15, CachedInputPer1M: 0.3},
	"claude-sonnet-4-6": {InputPer1M: 3, OutputPer1M: 15, CachedInputPer1M: 0.3},
	"haiku":             {InputPer1M: 1, OutputPer1M: 5, CachedInputPer1M: 0.1},
	"claude-haiku-4-5":  {InputPer1M: 1, OutputPer1M: 5, CachedInputPer1M: 0.1},

	// OpenAI — GPT-5 family. Cached read is ~10% of base.
	// Refresh from developers.openai.com/api/docs/models when prices change.
	"gpt-5.4":      {InputPer1M: 1.25, OutputPer1M: 10, CachedInputPer1M: 0.125},
	"gpt-5.5":      {InputPer1M: 1.25, OutputPer1M: 10, CachedInputPer1M: 0.125},
	"gpt-5.4-pro":  {InputPer1M: 5, OutputPer1M: 25, CachedInputPer1M: 0.5},
	"gpt-5.4-mini": {InputPer1M: 0.25, OutputPer1M: 2, CachedInputPer1M: 0.025},
	"gpt-5.4-nano": {InputPer1M: 0.05, OutputPer1M: 0.4, CachedInputPer1M: 0.005},
}

// CostUsd estimates run cost in USD from non-cached input tokens and output
// tokens. Returns 0 for unknown models. For runs with cached prompt tokens,
// prefer CostUsdWithCache.
func CostUsd(model string, inputTokens, outputTokens int) float64 {
	return CostUsdWithCache(model, inputTokens, 0, outputTokens)
}

// CostUsdWithCache estimates run cost in USD, charging cachedInputTokens at
// the provider's discounted cached rate when available. Falls back to the
// base input rate when CachedInputPer1M is zero.
func CostUsdWithCache(model string, inputTokens, cachedInputTokens, outputTokens int) float64 {
	p, ok := modelPrices[model]
	if !ok {
		return 0
	}
	cost := float64(inputTokens)/1_000_000*p.InputPer1M +
		float64(outputTokens)/1_000_000*p.OutputPer1M

	cachedRate := p.CachedInputPer1M
	if cachedRate == 0 {
		cachedRate = p.InputPer1M
	}
	cost += float64(cachedInputTokens) / 1_000_000 * cachedRate

	return cost
}
