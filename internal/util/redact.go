package util

// RedactKey returns a redacted version of a secret key, showing the first 7
// and last 4 characters separated by "...". Returns "(not set)" if empty and
// "****" if the key is too short to redact meaningfully.
func RedactKey(key string) string {
	if key == "" {
		return "(not set)"
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:7] + "..." + key[len(key)-4:]
}
