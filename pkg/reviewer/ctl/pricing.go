package ctl

// modelPrice is the per-1M-token list price for a single model.
// Cache-read / cache-write tiers are not modeled here — the per-1M base price
// is used as a rough estimate. For providers that report exact billing in the
// CLI output (Anthropic), prefer that value over CostUsd().
type modelPrice struct {
	InputPer1M  float64
	OutputPer1M float64
}

// modelPrices is the lookup table consulted by CostUsd. Lowercase model id;
// callers normalize before lookup.
//
// Numbers are illustrative — verify against the provider's published rates
// before relying on this for billing reconciliation. Anthropic returns exact
// per-run cost via its CLI, so its entries are mostly useful for validation.
var modelPrices = map[string]modelPrice{
	// Anthropic — Claude 4.x family
	"opus":              {InputPer1M: 5, OutputPer1M: 25},
	"claude-opus-4-7":   {InputPer1M: 5, OutputPer1M: 25},
	"claude-opus-4-6":   {InputPer1M: 5, OutputPer1M: 25},
	"sonnet":            {InputPer1M: 3, OutputPer1M: 15},
	"claude-sonnet-4-6": {InputPer1M: 3, OutputPer1M: 15},
	"haiku":             {InputPer1M: 1, OutputPer1M: 5},
	"claude-haiku-4-5":  {InputPer1M: 1, OutputPer1M: 5},

	// OpenAI — GPT-5 family. Approximate; refresh from
	// developers.openai.com/api/docs/models when prices change.
	"gpt-5.4":      {InputPer1M: 1.25, OutputPer1M: 10},
	"gpt-5.5":      {InputPer1M: 1.25, OutputPer1M: 10},
	"gpt-5.4-pro":  {InputPer1M: 5, OutputPer1M: 25},
	"gpt-5.4-mini": {InputPer1M: 0.25, OutputPer1M: 2},
	"gpt-5.4-nano": {InputPer1M: 0.05, OutputPer1M: 0.4},
}

// CostUsd estimates the run cost in USD from token counts using a static
// price table. Returns 0 for unknown models — callers should treat 0 as
// "no estimate available" rather than "free".
func CostUsd(model string, inputTokens, outputTokens int) float64 {
	p, ok := modelPrices[model]
	if !ok {
		return 0
	}
	return float64(inputTokens)/1_000_000*p.InputPer1M +
		float64(outputTokens)/1_000_000*p.OutputPer1M
}
