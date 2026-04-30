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
		name      string
		provider  string
		wantClaud bool
		wantErr   bool
	}{
		{"empty defaults to claude", "", true, false},
		{"claude explicit", "claude", true, false},
		{"codex deferred", "codex", false, true},
		{"unknown provider", "openai-direct", false, true},
		{"random garbage", "lol", false, true},
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
			if tt.wantClaud {
				_, ok := runner.(*ExecClaudeRunner)
				assert.True(t, ok, "expected *ExecClaudeRunner, got %T", runner)
			}
		})
	}
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
