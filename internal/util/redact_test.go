package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		want string
	}{
		{
			name: "empty key",
			key:  "",
			want: "(not set)",
		},
		{
			name: "short key",
			key:  "abc123",
			want: "****",
		},
		{
			name: "boundary key length 8",
			key:  "12345678",
			want: "****",
		},
		{
			name: "boundary key length 9",
			key:  "123456789",
			want: "1234567...6789",
		},
		{
			name: "normal key",
			key:  "abcdefg1234",
			want: "abcdefg...1234",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, RedactKey(tt.key))
		})
	}
}
