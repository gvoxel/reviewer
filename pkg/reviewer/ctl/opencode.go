package ctl

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// opencodeEvent is one JSON event from `opencode run --format json`.
// Only the fields we consume are declared — opencode emits more.
type opencodeEvent struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionID,omitempty"`
	Part      *opencodePart   `json:"part,omitempty"`
	Error     *opencodeError  `json:"error,omitempty"`
}

type opencodePart struct {
	Type   string           `json:"type"`
	Text   string           `json:"text,omitempty"`
	Reason string           `json:"reason,omitempty"`
	Tokens *opencodeTokens  `json:"tokens,omitempty"`
	Cost   *float64         `json:"cost,omitempty"`
}

type opencodeTokens struct {
	Input     int                `json:"input"`
	Output    int                `json:"output"`
	Reasoning int                `json:"reasoning"`
	Total     int                `json:"total"`
	Cache     opencodeCacheStats `json:"cache"`
}

type opencodeCacheStats struct {
	Read  int `json:"read"`
	Write int `json:"write"`
}

type opencodeError struct {
	Message string `json:"message"`
	Name    string `json:"name,omitempty"`
}

// ParseOpencodeResult parses the JSONL event stream from `opencode run --format json`.
//
// Opencode emits one JSON object per line. We aggregate `step_finish` tokens
// across all steps (multi-turn runs emit several), collect `text` parts as the
// final assistant message, and compute cost via pricing.go when opencode reports
// 0 (which happens for OAuth/subscription auth like ChatGPT Plus).
//
// Free models on the opencode-gateway sometimes do not emit step_finish at all;
// we still treat the run as successful if at least one text event was received.
func ParseOpencodeResult(data []byte, model string) (*RunResult, error) {
	if len(data) == 0 {
		return nil, errors.New("empty opencode output")
	}

	rr := &RunResult{
		Type:     claudeResultType,
		Subtype:  "success",
		Provider: ProviderOpencode,
	}

	var (
		textParts        []string
		sawStepFinish    bool
		sawText          bool
		sawErrorEvent    bool
		errorEventMsg    string
	)

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}

		var ev opencodeEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}

		if ev.SessionID != "" && rr.SessionID == "" {
			rr.SessionID = ev.SessionID
		}

		switch ev.Type {
		case "step_start":
			rr.NumTurns++

		case "text":
			if ev.Part != nil && ev.Part.Text != "" {
				textParts = append(textParts, ev.Part.Text)
				sawText = true
			}

		case "step_finish":
			sawStepFinish = true
			if ev.Part == nil {
				continue
			}
			if ev.Part.Reason != "" {
				rr.StopReason = ev.Part.Reason
			}
			if ev.Part.Tokens != nil {
				rr.Usage.InputTokens += ev.Part.Tokens.Input
				// Combine reasoning into output for billing parity with codex.
				rr.Usage.OutputTokens += ev.Part.Tokens.Output + ev.Part.Tokens.Reasoning
				rr.Usage.CacheReadInputTokens += ev.Part.Tokens.Cache.Read
				rr.Usage.CacheCreationInputTokens += ev.Part.Tokens.Cache.Write
			}
			if ev.Part.Cost != nil {
				rr.TotalCostUSD += *ev.Part.Cost
			}

		case "error":
			sawErrorEvent = true
			if ev.Error != nil && ev.Error.Message != "" {
				errorEventMsg = ev.Error.Message
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan opencode output: %w", err)
	}

	rr.Result = joinText(textParts)

	// Subscription-auth runs (OpenAI OAuth, etc.) report cost=0.
	// Fall back to the pricing table — same pattern as codex. Opencode uses
	// "provider/model" form (e.g. "openai/gpt-5.4-mini"); strip the prefix to
	// hit the bare model id in pricing.go.
	if rr.TotalCostUSD == 0 && (rr.Usage.InputTokens > 0 || rr.Usage.OutputTokens > 0) {
		rr.TotalCostUSD = CostUsdWithCache(stripProviderPrefix(model),
			rr.Usage.InputTokens, rr.Usage.CacheReadInputTokens, rr.Usage.OutputTokens)
	}

	if sawErrorEvent {
		rr.IsError = true
		rr.Subtype = "error"
		return rr, fmt.Errorf("opencode returned error: %s", errorEventMsg)
	}

	// A run with no text and no step_finish is not a successful completion.
	if !sawText && !sawStepFinish {
		return nil, errors.New("no text or step_finish events in opencode output")
	}

	return rr, nil
}

// stripProviderPrefix returns the model id without its opencode provider prefix
// ("openai/gpt-5.4-mini" → "gpt-5.4-mini"). Models without a slash are returned
// unchanged.
func stripProviderPrefix(model string) string {
	if idx := strings.IndexByte(model, '/'); idx >= 0 {
		return model[idx+1:]
	}
	return model
}

func joinText(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	// Multiple text events: concatenate. Opencode chunks long outputs across
	// several text events; joining preserves the full message.
	var buf bytes.Buffer
	for _, p := range parts {
		buf.WriteString(p)
	}
	return buf.String()
}

// OpencodeRunner runs the opencode CLI subprocess. Implements ProviderRunner.
type OpencodeRunner struct {
	Bin             string // path to opencode binary; empty → "opencode" on $PATH
	Model           string // provider/model form, e.g. "anthropic/claude-sonnet-4-6"
	Dir             string
	SessionID       string // --session <id>
	ContinueSession bool   // --continue
	Log             *slog.Logger
}

// Run executes `opencode run --format json` and parses the JSONL event stream.
// The prompt is supplied as the positional `message` argument (opencode does not
// read prompts from stdin).
func (r *OpencodeRunner) Run(ctx context.Context, prompt string) (*RunResult, error) {
	bin := r.Bin
	if bin == "" {
		bin = "opencode"
	}

	args := []string{
		"run",
		"--format", "json",
		"--model", r.Model,
		"--dangerously-skip-permissions",
	}

	if r.Dir != "" {
		args = append(args, "--dir", r.Dir)
	}

	if r.ContinueSession {
		args = append(args, "--continue")
	} else if r.SessionID != "" {
		args = append(args, "--session", r.SessionID)
	}

	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = r.Dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	r.Log.InfoContext(ctx, "running opencode",
		"bin", bin,
		"model", r.Model,
		"dir", r.Dir,
		"promptLen", len(prompt),
		"sessionId", r.SessionID,
		"continue", r.ContinueSession,
	)

	err := cmd.Run()

	r.Log.InfoContext(ctx, "opencode finished",
		"exitErr", err,
		"stdoutLen", stdout.Len(),
		"stderrLen", stderr.Len(),
	)

	if r.Log.Enabled(ctx, slog.LevelDebug) {
		if stderr.Len() > 0 {
			r.Log.DebugContext(ctx, "opencode stderr", "stderr", truncate(stderr.String(), 2000))
		}
		if stdout.Len() > 0 {
			r.Log.DebugContext(ctx, "opencode stdout", "stdout", truncate(stdout.String(), 2000))
		}
	}

	r.saveOutput(stdout.Bytes())

	if err != nil {
		r.Log.WarnContext(ctx, "opencode error",
			"stderr", truncate(stderr.String(), 2000),
		)
		// Try partial parse — opencode may have produced events before exiting.
		if stdout.Len() > 0 {
			if rr, parseErr := ParseOpencodeResult(stdout.Bytes(), r.Model); parseErr == nil {
				return rr, fmt.Errorf("opencode exited with error: %w", err)
			}
		}
		return nil, fmt.Errorf("opencode exited with error: %w (stderr: %s)", err, truncate(stderr.String(), 500))
	}

	if stdout.Len() == 0 {
		return nil, errors.New("opencode produced empty output")
	}

	rr, parseErr := ParseOpencodeResult(stdout.Bytes(), r.Model)
	if parseErr != nil {
		r.Log.WarnContext(ctx, "failed to parse opencode output",
			"err", parseErr,
			"stdoutPreview", truncate(stdout.String(), 500),
			"stderr", truncate(stderr.String(), 500),
		)
		return nil, parseErr
	}

	r.logResult(ctx, rr)
	return rr, nil
}

func (r *OpencodeRunner) logResult(ctx context.Context, rr *RunResult) {
	r.Log.InfoContext(ctx, "opencode result parsed",
		"cost", rr.TotalCostUSD,
		"turns", rr.NumTurns,
		"inputTokens", rr.Usage.InputTokens,
		"outputTokens", rr.Usage.OutputTokens,
		"cacheRead", rr.Usage.CacheReadInputTokens,
		"cacheWrite", rr.Usage.CacheCreationInputTokens,
		"sessionId", rr.SessionID,
		"stopReason", rr.StopReason,
	)

	if rr.IsError {
		r.Log.ErrorContext(ctx, "opencode run reported is_error", "sessionId", rr.SessionID)
	}
}

func (r *OpencodeRunner) saveOutput(data []byte) {
	if len(data) == 0 || r.Dir == "" {
		return
	}
	path := filepath.Join(r.Dir, "opencode-output.jsonl")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		r.Log.WarnContext(context.Background(), "failed to save opencode output", "err", err)
	}
}
