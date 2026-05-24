package ctl

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCostUsd(t *testing.T) {
	tests := []struct {
		name         string
		model        string
		inputTokens  int
		outputTokens int
		want         float64
	}{
		{"unknown model returns 0", "no-such-model", 1_000_000, 1_000_000, 0},
		{"empty model returns 0", "", 100, 100, 0},
		{"zero tokens", "opus", 0, 0, 0},

		// Anthropic — opus $5/$25
		{"opus 1M in / 1M out", "opus", 1_000_000, 1_000_000, 30},
		{"opus 100k in / 50k out", "opus", 100_000, 50_000, 0.5 + 1.25},
		{"claude-opus-4-7 alias", "claude-opus-4-7", 2_000_000, 1_000_000, 35},

		// Anthropic — sonnet $3/$15
		{"sonnet 1M / 1M", "sonnet", 1_000_000, 1_000_000, 18},

		// Anthropic — haiku $1/$5
		{"haiku 1M / 1M", "haiku", 1_000_000, 1_000_000, 6},

		// OpenAI — gpt-5.4 / gpt-5.5 ($1.25 / $10) per current table
		{"gpt-5.4 1M / 1M", "gpt-5.4", 1_000_000, 1_000_000, 11.25},
		{"gpt-5.5 1M / 1M", "gpt-5.5", 1_000_000, 1_000_000, 11.25},
		{"gpt-5.4-mini 1M / 1M", "gpt-5.4-mini", 1_000_000, 1_000_000, 2.25},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CostUsd(tt.model, tt.inputTokens, tt.outputTokens)
			assert.InDelta(t, tt.want, got, 0.0001)
		})
	}
}

func TestCostUsdWithCache(t *testing.T) {
	tests := []struct {
		name              string
		model             string
		inputTokens       int
		cachedInputTokens int
		outputTokens      int
		want              float64
	}{
		{"unknown model returns 0", "no-such", 100, 100, 100, 0},

		// gpt-5.4: input $1.25/M, cached $0.125/M, output $10/M.
		// 1M non-cached + 1M cached + 0 output: $1.25 + $0.125 = $1.375
		{"gpt-5.4 1M non-cached + 1M cached", "gpt-5.4", 1_000_000, 1_000_000, 0, 1.375},
		// 0 + 1M cached + 0: $0.125
		{"gpt-5.4 only cached input", "gpt-5.4", 0, 1_000_000, 0, 0.125},
		// 1M + 0 cached + 1M output: $1.25 + $10 = $11.25 (matches old CostUsd)
		{"gpt-5.4 zero cached equals CostUsd", "gpt-5.4", 1_000_000, 0, 1_000_000, 11.25},

		// opus: input $5, cached $0.5, output $25.
		// 100k + 1M cached + 50k: 0.5 + 0.5 + 1.25 = $2.25
		{"opus typical run with heavy cache", "opus", 100_000, 1_000_000, 50_000, 2.25},

		// haiku: input $1, cached $0.1, output $5
		{"haiku 1M cached only", "haiku", 0, 1_000_000, 0, 0.1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CostUsdWithCache(tt.model, tt.inputTokens, tt.cachedInputTokens, tt.outputTokens)
			assert.InDelta(t, tt.want, got, 0.0001)
		})
	}
}

// CostUsd should keep behaving as a thin wrapper that ignores cached tokens —
// equivalent to CostUsdWithCache with cachedInputTokens=0.
func TestCostUsdEquivalentToCostUsdWithCacheZero(t *testing.T) {
	models := []string{"opus", "sonnet", "haiku", "gpt-5.4", "gpt-5.5", "gpt-5.4-mini"}
	for _, m := range models {
		direct := CostUsd(m, 123_456, 78_910)
		viaCache := CostUsdWithCache(m, 123_456, 0, 78_910)
		assert.InDelta(t, direct, viaCache, 0.0001, "model %s: divergence", m)
	}
}
