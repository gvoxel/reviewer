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

// testProviderRunner returns a fixed RunResult from testdata.
type testProviderRunner struct {
	fixturePath string
}

func (r *testProviderRunner) Run(_ context.Context, _ string) (*RunResult, error) {
	data, err := os.ReadFile(r.fixturePath)
	if err != nil {
		return nil, err
	}
	return ParseClaudeResult(data)
}

// setupTestDir copies testdata files to a temp dir for upload tests.
func setupTestDir(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()

	files := []string{"review.json", "R1.architecture.md", "R2.code.md", "R3.security.md", "R4.tests.md"}
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join("testdata", f))
		require.NoError(t, err)
		err = os.WriteFile(filepath.Join(tmpDir, f), data, 0o644)
		require.NoError(t, err)
	}

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

	runner := &testProviderRunner{fixturePath: "testdata/claude_result.json"}
	c := NewController(cfg, runner, slog.Default())

	err := c.Review(context.Background())
	require.NoError(t, err)

	assert.True(t, promptCalled, "prompt was not fetched")
	assert.True(t, uploadedReview, "review was not uploaded")
}

// fakeRunner returns a fixed RunResult — used to exercise Controller paths
// that depend on the runner output without involving fixture files.
type fakeRunner struct {
	result *RunResult
}

func (r *fakeRunner) Run(_ context.Context, _ string) (*RunResult, error) {
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
	runner := &fakeRunner{result: &RunResult{Type: "result", Subtype: "success"}}
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
	runner := &fakeRunner{result: &RunResult{Type: "result", Subtype: "success", DurationMs: reportedMs}}
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
