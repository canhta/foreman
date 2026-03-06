package pipeline

import (
	"strings"
	"testing"

	"github.com/canhta/foreman/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestNeedsDecomposition(t *testing.T) {
	cfg := &models.DecomposeConfig{
		Enabled:          true,
		MaxTicketWords:   10,
		MaxScopeKeywords: 2,
	}

	tests := []struct {
		ticket *models.Ticket
		name   string
		want   bool
	}{
		{
			name:   "disabled config",
			ticket: &models.Ticket{Description: "a very long description that exceeds ten words for sure definitely"},
			want:   false,
		},
		{
			name:   "short ticket",
			ticket: &models.Ticket{Description: "fix the login button"},
			want:   false,
		},
		{
			name:   "long ticket exceeds word limit",
			ticket: &models.Ticket{Description: "implement the full user authentication system with OAuth2 support and email verification and password reset"},
			want:   true,
		},
		{
			name:   "scope keywords exceed threshold",
			ticket: &models.Ticket{Description: "add login and also add signup plus add password reset"},
			want:   true,
		},
		{
			name:   "child ticket never decomposes",
			ticket: &models.Ticket{Description: "implement the full user authentication system with OAuth2 support and email verification and password reset", DecomposeDepth: 1},
			want:   false,
		},
		{
			name:   "vague and long - no acceptance criteria",
			ticket: &models.Ticket{Description: strings.Repeat("word ", 101), AcceptanceCriteria: ""},
			want:   true,
		},
		{
			name:   "long but has acceptance criteria",
			ticket: &models.Ticket{Description: strings.Repeat("word ", 101), AcceptanceCriteria: "User can log in"},
			want:   true, // still exceeds word count
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := cfg
			if tt.name == "disabled config" {
				c = &models.DecomposeConfig{Enabled: false}
			}
			got := NeedsDecomposition(tt.ticket, c)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCountScopeKeywords(t *testing.T) {
	assert.Equal(t, 0, countScopeKeywords("fix the login button"))
	assert.Equal(t, 3, countScopeKeywords("add login and also add signup plus add password reset"))
	assert.Equal(t, 1, countScopeKeywords("do this additionally"))
}
