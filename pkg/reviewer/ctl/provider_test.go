package ctl

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRunner(t *testing.T) {
	log := slog.Default()
	tests := []struct {
		name     string
		provider string
		wantType string // "claude" | "codex" | ""
		wantErr  bool
	}{
		{"empty defaults to claude", "", "claude", false},
		{"claude explicit", "claude", "claude", false},
		{"codex implemented", "codex", "codex", false},
		{"unknown provider", "openai-direct", "", true},
		{"random garbage", "lol", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Provider: tt.provider, Model: "opus", Dir: "."}
			runner, err := NewRunner(cfg, log)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, runner)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, runner)
			switch tt.wantType {
			case "claude":
				_, ok := runner.(*ExecClaudeRunner)
				assert.True(t, ok, "expected *ExecClaudeRunner, got %T", runner)
			case "codex":
				_, ok := runner.(*CodexRunner)
				assert.True(t, ok, "expected *CodexRunner, got %T", runner)
			}
		})
	}
}

func TestNewRunnerCodexFieldsPropagated(t *testing.T) {
	log := slog.Default()
	cfg := &Config{
		Provider:        "codex",
		Model:           "gpt-5.4",
		Dir:             "/tmp/bar",
		SessionID:       "sess-xyz",
		ContinueSession: true,
	}
	runner, err := NewRunner(cfg, log)
	require.NoError(t, err)
	cr, ok := runner.(*CodexRunner)
	require.True(t, ok)
	assert.Equal(t, "gpt-5.4", cr.Model)
	assert.Equal(t, "/tmp/bar", cr.Dir)
	// Session/Continue are propagated for completeness but currently ignored
	// at Run time with a warning — see BACKLOG.md.
	assert.Equal(t, "sess-xyz", cr.SessionID)
	assert.True(t, cr.ContinueSession)
	assert.Same(t, log, cr.Log)
}

func TestNewRunnerClaudeFieldsPropagated(t *testing.T) {
	log := slog.Default()
	cfg := &Config{
		Provider:        "claude",
		Model:           "haiku",
		Dir:             "/tmp/foo",
		SessionID:       "sess-123",
		ContinueSession: true,
	}
	runner, err := NewRunner(cfg, log)
	require.NoError(t, err)
	cr, ok := runner.(*ExecClaudeRunner)
	require.True(t, ok)
	assert.Equal(t, "haiku", cr.Model)
	assert.Equal(t, "/tmp/foo", cr.Dir)
	assert.Equal(t, "sess-123", cr.SessionID)
	assert.True(t, cr.ContinueSession)
	assert.Same(t, log, cr.Log)
}
