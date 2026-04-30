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
