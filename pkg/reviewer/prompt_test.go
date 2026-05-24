package reviewer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The promptReviewJSON template should not contain Claude-specific
// instrumentation — duration and modelInfo (including cost) are filled
// by reviewctl after the run, regardless of provider.
func TestPromptReviewJSONIsModelAgnostic(t *testing.T) {
	tests := []struct {
		name     string
		fragment string
	}{
		{"no date-based duration measurement", "date +%s%3N"},
		{"no Anthropic price hardcode opus", "opus — $5/$25"},
		{"no Anthropic price hardcode sonnet", "sonnet — $3/$15"},
		{"no Anthropic price hardcode haiku", "haiku — $1/$5"},
		{"no instruction to compute costUsd", "рассчитай по формуле"},
		{"no instruction to fill modelInfo", "точное имя модели из твоей сессии"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.False(t,
				strings.Contains(promptReviewJSON, tt.fragment),
				"promptReviewJSON still contains Claude-specific fragment %q",
				tt.fragment,
			)
		})
	}
}
