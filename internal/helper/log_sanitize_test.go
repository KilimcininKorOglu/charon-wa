package helper

import (
	"strings"
	"testing"
)

func TestSanitizeLogValueStripsControlCharacters(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain text is unchanged", "worker-1", "worker-1"},
		{"newline is removed", "worker\nFAKE: injected", "workerFAKE: injected"},
		{"carriage return is removed", "worker\r\nFAKE", "workerFAKE"},
		{"tab becomes a space", "a\tb", "a b"},
		{"escape byte is removed", "a\x1b[31mred", "a[31mred"},
		{"del byte is removed", "a\x7fb", "ab"},
		{"turkish characters survive", "şğüöçİ", "şğüöçİ"},
		{"empty stays empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeLogValue(tt.in); got != tt.want {
				t.Errorf("SanitizeLogValue(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeLogValueTruncatesLongInput(t *testing.T) {
	got := SanitizeLogValue(strings.Repeat("a", logValueMaxLen+50))

	if !strings.HasSuffix(got, "...(truncated)") {
		t.Errorf("want a truncation marker, got %q", got[len(got)-20:])
	}

	body := strings.TrimSuffix(got, "...(truncated)")
	if len(body) != logValueMaxLen {
		t.Errorf("kept %d characters, want %d", len(body), logValueMaxLen)
	}
}

func TestSanitizeLogValueCountsRunesNotBytes(t *testing.T) {
	// Each ş is two bytes, so a byte-based limit would cut a rune in half.
	got := SanitizeLogValue(strings.Repeat("ş", logValueMaxLen+10))

	body := strings.TrimSuffix(got, "...(truncated)")
	if n := len([]rune(body)); n != logValueMaxLen {
		t.Errorf("kept %d runes, want %d", n, logValueMaxLen)
	}
	if strings.ContainsRune(body, '�') {
		t.Error("truncation split a multi-byte rune")
	}
}
