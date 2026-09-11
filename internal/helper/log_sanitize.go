package helper

import (
	"strings"
	"unicode/utf8"
)

// logValueMaxLen bounds a sanitized log value, counted in runes, so one
// oversized field cannot flood the log.
const logValueMaxLen = 512

// SanitizeLogValue makes a caller-supplied value safe to write into a log line.
// It removes every control character, so a newline in the value cannot forge a
// second log entry, and it truncates the result to logValueMaxLen.
func SanitizeLogValue(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	written := 0
	for _, r := range s {
		if written == logValueMaxLen {
			b.WriteString("...(truncated)")
			break
		}
		if r == '\t' {
			b.WriteByte(' ')
			written++
			continue
		}
		// Drop C0 controls, DEL, the C1 range, and any invalid byte.
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) || r == utf8.RuneError {
			continue
		}
		b.WriteRune(r)
		written++
	}

	return b.String()
}
