package ctl

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testProviderRunner returns a fixed RunResult from testdata. If seedDir is
// set, it also seeds review.json + R*.md fixtures into that directory before
// returning, mimicking what a real agent does inside its working directory.
type testProviderRunner struct {
	fixturePath string
	seedDir     string
}

func (r *testProviderRunner) Run(_ context.Context, _ string) (*RunResult, error) {
	if r.seedDir != "" {
		seedReviewFixtures(r.seedDir)
	}
	data, err := os.ReadFile(r.fixturePath)
	if err != nil {
		return nil, err
	}
	return ParseClaudeResult(data)
}

// seedReviewFixtures copies testdata review.json + R*.md into dir. Returns
// without raising — used in test runners to emulate the agent's writes.
// Errors are ignored intentionally; if a fixture is missing the test that
// follows will fail with a clear message anyway.
func seedReviewFixtures(dir string) {
	for _, f := range []string{"review.json", "R1.architecture.md", "R2.code.md", "R3.security.md", "R4.tests.md"} {
		data, err := os.ReadFile(filepath.Join("testdata", f))
		if err != nil {
			continue
		}
		_ = os.WriteFile(filepath.Join(dir, f), data, 0o644)
	}
}

// setupTestDir copies testdata files to a temp dir. Used by tests that don't
// go through Controller.Review (e.g. Upload-only paths) — the Review pipeline
// now wipes stale artifacts before runner.Run, so Review tests should seed via
// the runner mock instead.
func setupTestDir(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	seedReviewFixtures(tmpDir)
	return tmpDir
}

func TestController_Upload(t *testing.T) {
	var uploadedReview bool
	var uploadedFiles []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		// POST /v1/upload/{projectKey}/ — review
		if len(parts) == 3 && r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			var draft map[string]any
			json.Unmarshal(body, &draft)
			uploadedReview = true
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("42"))
			return
		}
		// POST /v1/upload/{projectKey}/{reviewId}/{type}/ — file
		if len(parts) == 5 && r.Method == http.MethodPost {
			uploadedFiles = append(uploadedFiles, parts[4])
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	tmpDir := setupTestDir(t)

	cfg := &Config{
		Key: "test-key",
		URL: srv.URL,
		Dir: tmpDir,
	}

	c := NewController(cfg, nil, slog.Default())
	err := c.Upload(context.Background())
	require.NoError(t, err)

	assert.True(t, uploadedReview, "review.json was not uploaded")
	assert.Len(t, uploadedFiles, 4)

	// Verify HTML was generated.
	htmlPath := filepath.Join(tmpDir, "review.html")
	_, err = os.Stat(htmlPath)
	assert.False(t, os.IsNotExist(err), "review.html was not generated")
}

func TestController_Review(t *testing.T) {
	promptCalled := false
	var uploadedReview bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		// GET /v1/prompt/{key}/
		if strings.HasPrefix(path, "/v1/prompt/") && r.Method == http.MethodGet {
			promptCalled = true
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Review %SOURCE_BRANCH% to %TARGET_BRANCH%"))
			return
		}
		// POST /v1/upload/{key}/
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) == 3 && r.Method == http.MethodPost {
			uploadedReview = true
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("42"))
			return
		}
		if len(parts) == 5 && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	tmpDir := setupTestDir(t)

	cfg := &Config{
		Key:          "test-key",
		URL:          srv.URL,
		Model:        "opus",
		Dir:          tmpDir,
		SourceBranch: "feature/test",
		TargetBranch: "master",
	}

	runner := &testProviderRunner{fixturePath: "testdata/claude_result.json", seedDir: tmpDir}
	c := NewController(cfg, runner, slog.Default())

	err := c.Review(context.Background())
	require.NoError(t, err)

	assert.True(t, promptCalled, "prompt was not fetched")
	assert.True(t, uploadedReview, "review was not uploaded")
}

// fakeRunner returns a fixed RunResult — used to exercise Controller paths
// that depend on the runner output. If seedDir is set, it also writes the
// review.json + R*.md fixtures to that directory, mimicking a real agent.
type fakeRunner struct {
	result  *RunResult
	seedDir string
}

func (r *fakeRunner) Run(_ context.Context, _ string) (*RunResult, error) {
	if r.seedDir != "" {
		seedReviewFixtures(r.seedDir)
	}
	return r.result, nil
}

func TestController_Review_DurationFallbackWhenRunnerReportsZero(t *testing.T) {
	var capturedReviewBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/v1/prompt/") && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("prompt"))
			return
		}
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) == 3 && r.Method == http.MethodPost {
			capturedReviewBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("99"))
			return
		}
		if len(parts) == 5 && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	tmpDir := setupTestDir(t)
	cfg := &Config{
		Key:   "test-key",
		URL:   srv.URL,
		Model: "gpt-5.4",
		Dir:   tmpDir,
	}

	// Runner reports DurationMs=0 — Controller must fall back to wall-clock.
	runner := &fakeRunner{result: &RunResult{Type: "result", Subtype: "success"}, seedDir: tmpDir}
	c := NewController(cfg, runner, slog.Default())

	err := c.Review(context.Background())
	require.NoError(t, err)
	require.NotNil(t, capturedReviewBody)

	var draft struct {
		Review struct {
			DurationMs int `json:"durationMs"`
		} `json:"review"`
	}
	require.NoError(t, json.Unmarshal(capturedReviewBody, &draft))
	assert.Greater(t, draft.Review.DurationMs, 0,
		"durationMs must be filled by Controller when runner reports 0")
}

func TestController_Review_DurationKeptWhenRunnerReportsNonZero(t *testing.T) {
	var capturedReviewBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/v1/prompt/") && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("prompt"))
			return
		}
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) == 3 && r.Method == http.MethodPost {
			capturedReviewBody, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("99"))
			return
		}
		if len(parts) == 5 && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	tmpDir := setupTestDir(t)
	cfg := &Config{Key: "k", URL: srv.URL, Model: "opus", Dir: tmpDir}

	const reportedMs = 12345
	runner := &fakeRunner{result: &RunResult{Type: "result", Subtype: "success", DurationMs: reportedMs}, seedDir: tmpDir}
	c := NewController(cfg, runner, slog.Default())

	require.NoError(t, c.Review(context.Background()))
	require.NotNil(t, capturedReviewBody)

	var draft struct {
		Review struct {
			DurationMs int `json:"durationMs"`
		} `json:"review"`
	}
	require.NoError(t, json.Unmarshal(capturedReviewBody, &draft))
	assert.Equal(t, reportedMs, draft.Review.DurationMs,
		"runner-reported DurationMs must not be overwritten")
}

func TestRemoveReviewArtifacts(t *testing.T) {
	tmp := t.TempDir()

	// Files we expect to be removed.
	stale := []string{
		"review.json",
		"review.html",
		"codex-output.jsonl",
		"claude-output.json",
		"R1.architecture.ru.md",
		"R2.code.ru.md",
		"R3.security.md",
		"R4.tests.en.md",
		"R5.operability.md",
	}
	// Files we expect to survive (user's repo content).
	keep := []string{
		"main.go",
		"README.md",
		"R6.notreview.md", // outside R[1-5] range
		"r1.architecture.md", // lowercase prefix
		"some.md",
	}

	for _, name := range append(stale, keep...) {
		require.NoError(t, os.WriteFile(filepath.Join(tmp, name), []byte("x"), 0o644))
	}

	removeReviewArtifacts(tmp, slog.Default())

	for _, name := range stale {
		_, err := os.Stat(filepath.Join(tmp, name))
		assert.True(t, os.IsNotExist(err), "stale file %q must be removed", name)
	}
	for _, name := range keep {
		_, err := os.Stat(filepath.Join(tmp, name))
		assert.NoError(t, err, "non-artifact file %q must remain", name)
	}
}

func TestRemoveReviewArtifacts_EmptyDirNoop(t *testing.T) {
	// Should not panic when dir is empty or doesn't exist — best-effort.
	removeReviewArtifacts("", slog.Default())
	removeReviewArtifacts("/nonexistent/path/that/does/not/exist", slog.Default())
}

func TestController_Comment(t *testing.T) {
	var commentPosted bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/notes") {
			commentPosted = true
			w.WriteHeader(http.StatusCreated)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/discussions") {
			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	tmpDir := setupTestDir(t)

	cfg := &Config{
		Key:         "test-key",
		URL:         "https://reviewer.example.com",
		Dir:         tmpDir,
		ReviewID:    42,
		GitLabToken: "test-token",
		GitLabURL:   srv.URL,
		ProjectID:   "123",
		MRIID:       "42",
		DiffBaseSHA: "base-sha",
		Commit:      "head-sha",
	}

	c := NewController(cfg, nil, slog.Default())
	err := c.Comment(context.Background())
	require.NoError(t, err)

	assert.True(t, commentPosted, "MR comment was not posted")
}
