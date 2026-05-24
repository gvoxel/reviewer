package ctl

import (
	"fmt"
	"log/slog"
)

// Provider names accepted by the --provider flag.
const (
	ProviderClaude   = "claude"
	ProviderCodex    = "codex"
	ProviderOpencode = "opencode"
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
		// Codex session resume (--continue / --session) is not yet wired —
		// CodexRunner logs a warning and ignores those fields. See BACKLOG.md.
		return &CodexRunner{
			Model:           cfg.Model,
			Dir:             cfg.Dir,
			SessionID:       cfg.SessionID,
			ContinueSession: cfg.ContinueSession,
			Log:             log,
		}, nil

	case ProviderOpencode:
		return &OpencodeRunner{
			Bin:             cfg.OpencodeBin,
			Model:           cfg.Model,
			Dir:             cfg.Dir,
			SessionID:       cfg.SessionID,
			ContinueSession: cfg.ContinueSession,
			Log:             log,
		}, nil

	default:
		return nil, fmt.Errorf("unknown provider %q (supported: %s, %s, %s)",
			provider, ProviderClaude, ProviderCodex, ProviderOpencode)
	}
}
