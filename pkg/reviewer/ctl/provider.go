package ctl

import (
	"errors"
	"fmt"
	"log/slog"
)

// Provider names accepted by the --provider flag.
const (
	ProviderClaude = "claude"
	ProviderCodex  = "codex"
)

// NewRunner builds the ProviderRunner implementation that matches cfg.Provider.
// An empty cfg.Provider defaults to Claude.
func NewRunner(cfg *Config, log *slog.Logger) (ProviderRunner, error) {
	provider := cfg.Provider
	if provider == "" {
		provider = ProviderClaude
	}

	switch provider {
	case ProviderClaude:
		return &ExecClaudeRunner{
			Model:           cfg.Model,
			Dir:             cfg.Dir,
			SessionID:       cfg.SessionID,
			ContinueSession: cfg.ContinueSession,
			Log:             log,
		}, nil

	case ProviderCodex:
		// CodexRunner ships in a follow-up commit. Codex CLI does support
		// session resume via "codex exec resume --last|<id>", but that is
		// deferred — see BACKLOG.md.
		return nil, errors.New("provider codex: not implemented yet")

	default:
		return nil, fmt.Errorf("unknown provider %q (supported: %s, %s)", provider, ProviderClaude, ProviderCodex)
	}
}
