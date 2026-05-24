package ctl

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOpencodeResult_HappyPath(t *testing.T) {
	data, err := os.ReadFile("testdata/opencode/probe_hello.jsonl")
	require.NoError(t, err)

	rr, err := ParseOpencodeResult(data, "gpt-5.4-mini")
	require.NoError(t, err)
	require.NotNil(t, rr)

	assert.Equal(t, ProviderOpencode, rr.Provider)
	assert.Equal(t, "hello", rr.Result)
	assert.Equal(t, "ses_test01", rr.SessionID)
	assert.Equal(t, 1, rr.NumTurns)
	// Opencode's tokens.input is already non-cached (input + output + reasoning
	// + cache.write + cache.read = total), unlike codex. No subtraction needed.
	assert.Equal(t, 21392, rr.Usage.InputTokens)
	// 7 output + 14 reasoning combined for billing parity with codex.
	assert.Equal(t, 21, rr.Usage.OutputTokens)
	assert.Equal(t, 0, rr.Usage.CacheReadInputTokens)
	assert.Equal(t, 0, rr.Usage.CacheCreationInputTokens)
	assert.Equal(t, "stop", rr.StopReason)
	assert.False(t, rr.IsError)

	// Live capture had cost=0 (OAuth auth) — fallback to pricing table.
	expectedCost := CostUsdWithCache("gpt-5.4-mini", 21392, 0, 21)
	assert.InDelta(t, expectedCost, rr.TotalCostUSD, 0.0001)
	assert.Greater(t, rr.TotalCostUSD, 0.0)

	mi := rr.ToModelInfo("gpt-5.4-mini")
	assert.Equal(t, ProviderOpencode, mi.Provider)
}

func TestParseOpencodeResult_NativeCostPreferredOverPricingTable(t *testing.T) {
	// When opencode reports a non-zero cost (API-key auth like Anthropic),
	// we must keep it as-is and not overwrite from the pricing table.
	stream := `{"type":"step_start","sessionID":"ses_x","part":{"type":"step-start"}}
{"type":"text","sessionID":"ses_x","part":{"type":"text","text":"hi"}}
{"type":"step_finish","sessionID":"ses_x","part":{"reason":"stop","type":"step-finish","tokens":{"total":11168,"input":2,"output":34,"cache":{"write":11132,"read":0}},"cost":0.014087}}
`
	rr, err := ParseOpencodeResult([]byte(stream), "haiku")
	require.NoError(t, err)
	assert.InDelta(t, 0.014087, rr.TotalCostUSD, 0.0000001)
	assert.Equal(t, 11132, rr.Usage.CacheCreationInputTokens)
	assert.Equal(t, 0, rr.Usage.CacheReadInputTokens)
}

func TestParseOpencodeResult_MultipleStepsAggregate(t *testing.T) {
	// Multi-turn run with two step_finish events — tokens and cost sum.
	stream := strings.Join([]string{
		`{"type":"step_start","sessionID":"ses_m","part":{"type":"step-start"}}`,
		`{"type":"text","sessionID":"ses_m","part":{"type":"text","text":"first "}}`,
		`{"type":"step_finish","sessionID":"ses_m","part":{"reason":"tool_use","type":"step-finish","tokens":{"input":100,"output":20,"reasoning":5,"cache":{"read":10,"write":50}},"cost":0.001}}`,
		`{"type":"step_start","sessionID":"ses_m","part":{"type":"step-start"}}`,
		`{"type":"text","sessionID":"ses_m","part":{"type":"text","text":"second"}}`,
		`{"type":"step_finish","sessionID":"ses_m","part":{"reason":"stop","type":"step-finish","tokens":{"input":200,"output":30,"reasoning":15,"cache":{"read":40,"write":0}},"cost":0.002}}`,
		"",
	}, "\n")

	rr, err := ParseOpencodeResult([]byte(stream), "gpt-5.4")
	require.NoError(t, err)
	assert.Equal(t, "first second", rr.Result)
	assert.Equal(t, 2, rr.NumTurns)
	assert.Equal(t, 300, rr.Usage.InputTokens)
	assert.Equal(t, 70, rr.Usage.OutputTokens) // 20+5+30+15
	assert.Equal(t, 50, rr.Usage.CacheReadInputTokens)
	assert.Equal(t, 50, rr.Usage.CacheCreationInputTokens)
	assert.InDelta(t, 0.003, rr.TotalCostUSD, 0.0000001)
	assert.Equal(t, "stop", rr.StopReason) // last step_finish wins
}

func TestParseOpencodeResult_NoStepFinishStillSucceeds(t *testing.T) {
	// Free models on the opencode-gateway sometimes don't emit step_finish;
	// a run with just text events is still a successful answer.
	stream := `{"type":"step_start","sessionID":"ses_f","part":{"type":"step-start"}}
{"type":"text","sessionID":"ses_f","part":{"type":"text","text":"hello"}}
`
	rr, err := ParseOpencodeResult([]byte(stream), "gpt-5.4-mini")
	require.NoError(t, err)
	assert.Equal(t, "hello", rr.Result)
	assert.Equal(t, "ses_f", rr.SessionID)
	assert.Equal(t, 1, rr.NumTurns)
	assert.Equal(t, 0, rr.Usage.InputTokens)
	assert.False(t, rr.IsError)
}

func TestParseOpencodeResult_EmptyInput(t *testing.T) {
	_, err := ParseOpencodeResult(nil, "gpt-5.4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestParseOpencodeResult_NoTextNoStepFinish(t *testing.T) {
	// Only step_start — not a successful completion.
	stream := `{"type":"step_start","sessionID":"ses_z","part":{"type":"step-start"}}
`
	_, err := ParseOpencodeResult([]byte(stream), "gpt-5.4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no text")
}

func TestParseOpencodeResult_MalformedLinesTolerated(t *testing.T) {
	stream := `{"type":"step_start","sessionID":"ses_q","part":{"type":"step-start"}}
{broken json
{"type":"text","sessionID":"ses_q","part":{"type":"text","text":"recovered"}}
{"type":"step_finish","sessionID":"ses_q","part":{"reason":"stop","tokens":{"input":1,"output":1}}}
`
	rr, err := ParseOpencodeResult([]byte(stream), "gpt-5.4")
	require.NoError(t, err)
	assert.Equal(t, "recovered", rr.Result)
}

func TestParseOpencodeResult_IgnoresNonJSONNoise(t *testing.T) {
	stream := `Shell cwd was reset to /tmp
{"type":"step_start","sessionID":"ses_n","part":{"type":"step-start"}}
random log line
{"type":"text","sessionID":"ses_n","part":{"type":"text","text":"ok"}}
{"type":"step_finish","sessionID":"ses_n","part":{"reason":"stop","tokens":{"input":5,"output":2}}}
trailing junk
`
	rr, err := ParseOpencodeResult([]byte(stream), "gpt-5.4")
	require.NoError(t, err)
	assert.Equal(t, "ok", rr.Result)
	assert.Equal(t, "ses_n", rr.SessionID)
}

func TestParseOpencodeResult_ErrorEvent(t *testing.T) {
	stream := `{"type":"step_start","sessionID":"ses_e","part":{"type":"step-start"}}
{"type":"error","sessionID":"ses_e","error":{"message":"rate limit exceeded","name":"RateLimitError"}}
`
	rr, err := ParseOpencodeResult([]byte(stream), "gpt-5.4")
	require.Error(t, err)
	require.NotNil(t, rr)
	assert.True(t, rr.IsError)
	assert.Equal(t, "error", rr.Subtype)
	assert.Contains(t, err.Error(), "rate limit exceeded")
}

func TestParseOpencodeResult_ProviderPrefixStrippedForPricing(t *testing.T) {
	// Opencode passes "openai/gpt-5.4-mini" but pricing.go has bare "gpt-5.4-mini".
	// The fallback must strip the prefix before lookup, otherwise cost stays at 0.
	data, err := os.ReadFile("testdata/opencode/probe_hello.jsonl")
	require.NoError(t, err)

	rr, err := ParseOpencodeResult(data, "openai/gpt-5.4-mini")
	require.NoError(t, err)
	assert.Greater(t, rr.TotalCostUSD, 0.0, "prefixed model must resolve via pricing table")

	bare, err := ParseOpencodeResult(data, "gpt-5.4-mini")
	require.NoError(t, err)
	assert.InDelta(t, bare.TotalCostUSD, rr.TotalCostUSD, 0.0000001)
}

func TestParseOpencodeResult_CostUsesGivenModel(t *testing.T) {
	data, err := os.ReadFile("testdata/opencode/probe_hello.jsonl")
	require.NoError(t, err)

	cheap, err := ParseOpencodeResult(data, "gpt-5.4-nano")
	require.NoError(t, err)
	expensive, err := ParseOpencodeResult(data, "gpt-5.4-pro")
	require.NoError(t, err)

	assert.Less(t, cheap.TotalCostUSD, expensive.TotalCostUSD,
		"gpt-5.4-nano must cost less than gpt-5.4-pro for the same tokens")
	assert.Greater(t, cheap.TotalCostUSD, 0.0)
}
