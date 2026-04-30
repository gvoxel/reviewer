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

// codexEvent is one JSON event from `codex exec --json`. Only the fields we
// actually consume are declared — codex emits more types that we ignore.
type codexEvent struct {
	Type     string      `json:"type"`
	ThreadID string      `json:"thread_id,omitempty"`
	Item     *codexItem  `json:"item,omitempty"`
	Usage    *codexUsage `json:"usage,omitempty"`
	Error    *codexError `json:"error,omitempty"`
}

type codexItem struct {
	ID   string `json:"id"`
	Type string `json:"type"` // "agent_message", ...
	Text string `json:"text"`
}

type codexUsage struct {
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
}

type codexError struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// ParseCodexResult parses the JSONL event stream produced by `codex exec --json`.
// Lines that are not JSON objects (e.g. "Shell cwd was reset to ...") are ignored.
// Returns a RunResult with Provider=ProviderCodex and TotalCostUSD computed via
// CostUsd(model, in, out) — Codex CLI does not report billing.
func ParseCodexResult(data []byte, model string) (*RunResult, error) {
	if len(data) == 0 {
		return nil, errors.New("empty codex output")
	}

	rr := &RunResult{
		Type:     claudeResultType,
		Subtype:  "success",
		Provider: ProviderCodex,
	}

	var (
		lastAgentMessage string
		sawTurnCompleted bool
		turnFailedMsg    string
	)

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}

		var ev codexEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}

		switch ev.Type {
		case "thread.started":
			if ev.ThreadID != "" {
				rr.SessionID = ev.ThreadID
			}
		case "turn.started":
			rr.NumTurns++
		case "item.completed":
			if ev.Item != nil && ev.Item.Type == "agent_message" {
				lastAgentMessage = ev.Item.Text
			}
		case "turn.completed":
			sawTurnCompleted = true
			if ev.Usage != nil {
				rr.Usage.InputTokens = ev.Usage.InputTokens
				rr.Usage.CacheReadInputTokens = ev.Usage.CachedInputTokens
				// Reasoning tokens are billed as output — combine for cost parity.
				rr.Usage.OutputTokens = ev.Usage.OutputTokens + ev.Usage.ReasoningOutputTokens
			}
			rr.StopReason = "end_turn"
		case "turn.failed":
			rr.IsError = true
			rr.Subtype = "error"
			if ev.Error != nil && ev.Error.Message != "" {
				turnFailedMsg = ev.Error.Message
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan codex output: %w", err)
	}

	if !sawTurnCompleted && !rr.IsError {
		return nil, errors.New("no turn.completed event in codex output")
	}

	rr.Result = lastAgentMessage
	rr.TotalCostUSD = CostUsd(model, rr.Usage.InputTokens, rr.Usage.OutputTokens)

	if rr.IsError && turnFailedMsg != "" {
		return rr, fmt.Errorf("codex returned error: %s", turnFailedMsg)
	}

	return rr, nil
}

// CodexRunner runs the OpenAI Codex CLI subprocess. Implements ProviderRunner.
type CodexRunner struct {
	Model           string
	Dir             string
	SessionID       string // currently ignored — see BACKLOG.md
	ContinueSession bool   // currently ignored — see BACKLOG.md
	Log             *slog.Logger
}

// Run executes the codex CLI and returns the parsed RunResult. The prompt is
// supplied via stdin.
func (r *CodexRunner) Run(ctx context.Context, prompt string) (*RunResult, error) {
	if r.SessionID != "" || r.ContinueSession {
		r.Log.WarnContext(ctx,
			"codex provider does not yet support session resume; ignoring --session/--continue",
			"sessionId", r.SessionID,
			"continueSession", r.ContinueSession,
		)
	}

	args := []string{
		"exec",
		"--json",
		"--model", r.Model,
		"--dangerously-bypass-approvals-and-sandbox",
	}
	if r.Dir != "" {
		args = append(args, "--cd", r.Dir)
	}
	args = append(args, "-") // read prompt from stdin

	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Dir = r.Dir
	cmd.Stdin = strings.NewReader(prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	r.Log.InfoContext(ctx, "running codex",
		"model", r.Model,
		"dir", r.Dir,
		"promptLen", len(prompt),
		"args", args,
	)

	err := cmd.Run()

	r.Log.InfoContext(ctx, "codex finished",
		"exitErr", err,
		"stdoutLen", stdout.Len(),
		"stderrLen", stderr.Len(),
	)

	if r.Log.Enabled(ctx, slog.LevelDebug) {
		if stderr.Len() > 0 {
			r.Log.DebugContext(ctx, "codex stderr", "stderr", truncate(stderr.String(), 2000))
		}
		if stdout.Len() > 0 {
			r.Log.DebugContext(ctx, "codex stdout", "stdout", truncate(stdout.String(), 2000))
		}
	}

	r.saveOutput(stdout.Bytes())

	if err != nil {
		r.Log.WarnContext(ctx, "codex error",
			"stderr", truncate(stderr.String(), 2000),
		)
		// Try partial parse — codex may have produced events before exiting.
		if stdout.Len() > 0 {
			if rr, parseErr := ParseCodexResult(stdout.Bytes(), r.Model); parseErr == nil {
				return rr, fmt.Errorf("codex exited with error: %w", err)
			}
		}
		return nil, fmt.Errorf("codex exited with error: %w (stderr: %s)", err, truncate(stderr.String(), 500))
	}

	if stdout.Len() == 0 {
		return nil, errors.New("codex produced empty output")
	}

	rr, parseErr := ParseCodexResult(stdout.Bytes(), r.Model)
	if parseErr != nil {
		r.Log.WarnContext(ctx, "failed to parse codex output",
			"err", parseErr,
			"stdoutPreview", truncate(stdout.String(), 500),
			"stderr", truncate(stderr.String(), 500),
		)
		return nil, parseErr
	}

	r.logResult(ctx, rr)
	return rr, nil
}

func (r *CodexRunner) logResult(ctx context.Context, rr *RunResult) {
	r.Log.InfoContext(ctx, "codex result parsed",
		"cost", rr.TotalCostUSD,
		"turns", rr.NumTurns,
		"inputTokens", rr.Usage.InputTokens,
		"outputTokens", rr.Usage.OutputTokens,
		"cacheRead", rr.Usage.CacheReadInputTokens,
		"sessionId", rr.SessionID,
	)

	if rr.IsError {
		r.Log.ErrorContext(ctx, "codex run reported is_error", "sessionId", rr.SessionID)
	}
}

func (r *CodexRunner) saveOutput(data []byte) {
	if len(data) == 0 || r.Dir == "" {
		return
	}
	path := filepath.Join(r.Dir, "codex-output.jsonl")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		r.Log.WarnContext(context.Background(), "failed to save codex output", "err", err)
	}
}
