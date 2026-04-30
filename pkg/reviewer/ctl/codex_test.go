package ctl

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCodexResult_HappyPath(t *testing.T) {
	data, err := os.ReadFile("testdata/codex/probe_hello.jsonl")
	require.NoError(t, err)

	rr, err := ParseCodexResult(data, "gpt-5.4")
	require.NoError(t, err)
	require.NotNil(t, rr)

	assert.Equal(t, ProviderCodex, rr.Provider)
	assert.Equal(t, "hello world", rr.Result)
	assert.Equal(t, "019dddcb-5ae0-7ea0-badb-1f3b041b07a4", rr.SessionID)
	assert.Equal(t, 1, rr.NumTurns)
	assert.Equal(t, 20535, rr.Usage.InputTokens)
	assert.Equal(t, 5504, rr.Usage.CacheReadInputTokens)
	// 17 output + 9 reasoning combined for billing parity.
	assert.Equal(t, 26, rr.Usage.OutputTokens)
	assert.False(t, rr.IsError)
	assert.Equal(t, "end_turn", rr.StopReason)

	// Cost is computed from the static price table.
	mi := rr.ToModelInfo("gpt-5.4")
	assert.Equal(t, ProviderCodex, mi.Provider)
	assert.InDelta(t, CostUsd("gpt-5.4", 20535, 26), mi.CostUsd, 0.0001)
}

func TestParseCodexResult_CostUsesGivenModel(t *testing.T) {
	data, err := os.ReadFile("testdata/codex/probe_hello.jsonl")
	require.NoError(t, err)

	cheap, err := ParseCodexResult(data, "gpt-5.4-mini")
	require.NoError(t, err)
	expensive, err := ParseCodexResult(data, "gpt-5.4")
	require.NoError(t, err)

	assert.Less(t, cheap.TotalCostUSD, expensive.TotalCostUSD,
		"gpt-5.4-mini must cost less than gpt-5.4 for the same tokens")
	assert.Greater(t, cheap.TotalCostUSD, 0.0)
}

func TestParseCodexResult_IgnoresNonJSONNoise(t *testing.T) {
	stream := `Shell cwd was reset to /Users/x/y
{"type":"thread.started","thread_id":"t-1"}
some random log line that is not JSON
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"i0","type":"agent_message","text":"ok"}}
{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":0,"output_tokens":5,"reasoning_output_tokens":0}}
trailing junk
`
	rr, err := ParseCodexResult([]byte(stream), "gpt-5.4")
	require.NoError(t, err)
	assert.Equal(t, "ok", rr.Result)
	assert.Equal(t, "t-1", rr.SessionID)
	assert.Equal(t, 10, rr.Usage.InputTokens)
	assert.Equal(t, 5, rr.Usage.OutputTokens)
}

func TestParseCodexResult_MalformedLinesTolerated(t *testing.T) {
	// One malformed JSON line in the middle should not abort parsing.
	stream := `{"type":"thread.started","thread_id":"t-1"}
{"type":"turn.started"}
{"type":"item.completed","item":{broken json
{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"recovered"}}
{"type":"turn.completed","usage":{"input_tokens":5,"output_tokens":2}}
`
	rr, err := ParseCodexResult([]byte(stream), "gpt-5.4")
	require.NoError(t, err)
	assert.Equal(t, "recovered", rr.Result)
}

func TestParseCodexResult_TurnFailed(t *testing.T) {
	stream := `{"type":"thread.started","thread_id":"t-1"}
{"type":"turn.started"}
{"type":"turn.failed","error":{"message":"rate limit exceeded","code":"rate_limit"}}
`
	rr, err := ParseCodexResult([]byte(stream), "gpt-5.4")
	require.Error(t, err)
	require.NotNil(t, rr)
	assert.True(t, rr.IsError)
	assert.Equal(t, "error", rr.Subtype)
	assert.Contains(t, err.Error(), "rate limit exceeded")
}

func TestParseCodexResult_NoTurnCompleted(t *testing.T) {
	// Stream that ends before turn.completed and without turn.failed —
	// should be flagged as an incomplete run.
	stream := `{"type":"thread.started","thread_id":"t-1"}
{"type":"turn.started"}
`
	_, err := ParseCodexResult([]byte(stream), "gpt-5.4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no turn.completed")
}

func TestParseCodexResult_EmptyInput(t *testing.T) {
	_, err := ParseCodexResult(nil, "gpt-5.4")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestParseCodexResult_LastAgentMessageWins(t *testing.T) {
	// Multiple agent_message events — only the last one is the final answer.
	stream := `{"type":"thread.started","thread_id":"t-1"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"i0","type":"agent_message","text":"thinking..."}}
{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"final answer"}}
{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":3}}
`
	rr, err := ParseCodexResult([]byte(stream), "gpt-5.4")
	require.NoError(t, err)
	assert.Equal(t, "final answer", rr.Result)
}

func TestParseCodexResult_UnknownItemTypesIgnored(t *testing.T) {
	// Items of types other than "agent_message" must not become rr.Result.
	stream := `{"type":"thread.started","thread_id":"t-1"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"i0","type":"file_change","text":"modified main.go"}}
{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"done"}}
{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}
`
	rr, err := ParseCodexResult([]byte(stream), "gpt-5.4")
	require.NoError(t, err)
	assert.Equal(t, "done", rr.Result)
}

func TestParseCodexResult_MultipleTurns(t *testing.T) {
	// Two turns — usage from the last turn.completed is what we carry forward.
	stream := strings.Join([]string{
		`{"type":"thread.started","thread_id":"t-1"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"id":"i0","type":"agent_message","text":"first"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":100,"output_tokens":50}}`,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"second"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":200,"output_tokens":80}}`,
		"",
	}, "\n")

	rr, err := ParseCodexResult([]byte(stream), "gpt-5.4")
	require.NoError(t, err)
	assert.Equal(t, "second", rr.Result)
	assert.Equal(t, 2, rr.NumTurns)
	assert.Equal(t, 200, rr.Usage.InputTokens)
	assert.Equal(t, 80, rr.Usage.OutputTokens)
}
