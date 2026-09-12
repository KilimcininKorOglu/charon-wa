package helper

import (
	"errors"
	"testing"

	"charon/config"
)

// setRegion points the normaliser at one default region for the duration of a
// single subtest.
func setRegion(t *testing.T, region string) {
	t.Helper()
	previous := config.PhoneRegion()
	config.SetPhoneRegion(region)
	t.Cleanup(func() { config.SetPhoneRegion(previous) })
}

func TestExtractPhoneFromJID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"905551234567@s.whatsapp.net", "905551234567"},
		{"905551234567:43@s.whatsapp.net", "905551234567"},
		{"905123456789@s.whatsapp.net", "905123456789"},
		{"905551234567", "905551234567"},
		{"", ""},
	}

	for _, tt := range tests {
		result := ExtractPhoneFromJID(tt.input)
		if result != tt.expected {
			t.Errorf("ExtractPhoneFromJID(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestNormalizePhone(t *testing.T) {
	tests := []struct {
		name    string
		region  string
		input   string
		want    string
		wantErr error
	}{
		// ── National numbers of the default region ───────────────────────────
		{name: "TR trunk prefix", region: "TR", input: "05551234567", want: "905551234567"},
		{name: "TR without trunk prefix", region: "TR", input: "5551234567", want: "905551234567"},
		{name: "TR with separators", region: "TR", input: "0555 123 45 67", want: "905551234567"},
		{name: "TR already international", region: "TR", input: "905551234567", want: "905551234567"},
		{name: "TR fixed line accepted", region: "TR", input: "+90 212 345 6789", want: "902123456789"},
		{name: "ID trunk prefix", region: "ID", input: "08123456789", want: "628123456789"},

		// ── International escapes ────────────────────────────────────────────
		{name: "IDD prefix resolves to the same number", region: "TR", input: "0090 555 123 45 67", want: "905551234567"},
		{name: "IDD prefix to a foreign number", region: "TR", input: "00447911123456", want: "447911123456"},
		{name: "plus prefix to a foreign number", region: "TR", input: "+1 202 555 0134", want: "12025550134"},
		{name: "foreign number without a plus", region: "TR", input: "628123456789", want: "628123456789"},

		// ── Numbers that are not valid for their country ─────────────────────
		{name: "foreign length is not a local number", region: "TR", input: "2025550134", wantErr: ErrPhoneInvalid},
		{name: "unassigned TR prefix", region: "TR", input: "01111111111", wantErr: ErrPhoneInvalid},
		{name: "too short", region: "TR", input: "905", wantErr: ErrPhoneInvalid},
		{name: "too long", region: "TR", input: "9055512345678901234", wantErr: ErrPhoneInvalid},
		{name: "letters", region: "TR", input: "905abc1234567", wantErr: ErrPhoneInvalid},
		{name: "unassigned country code", region: "", input: "+9991234567", wantErr: ErrPhoneUnparseable},

		// ── No default region: full international format only ────────────────
		{name: "no region, plus prefix", region: "", input: "+905551234567", want: "905551234567"},
		{name: "no region, bare digits rejected", region: "", input: "905551234567", wantErr: ErrPhoneNoRegion},
		{name: "no region, local format rejected", region: "", input: "05551234567", wantErr: ErrPhoneNoRegion},

		// ── Empty input ──────────────────────────────────────────────────────
		{name: "empty", region: "TR", input: "", wantErr: ErrPhoneEmpty},
		{name: "blank", region: "TR", input: "   ", wantErr: ErrPhoneEmpty},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRegion(t, tt.region)

			got, err := NormalizePhone(tt.input)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("NormalizePhone(%q) error = %v, want %v", tt.input, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizePhone(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("NormalizePhone(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizePhoneIsIdempotent(t *testing.T) {
	setRegion(t, "TR")

	first, err := NormalizePhone("0555 123 45 67")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	second, err := NormalizePhone(first)
	if err != nil {
		t.Fatalf("unexpected error on the second pass: %v", err)
	}
	if second != first {
		t.Errorf("second pass = %q, want %q", second, first)
	}
}

func TestFormatPhoneNumber(t *testing.T) {
	setRegion(t, "TR")

	jid, err := FormatPhoneNumber("0555 123 45 67")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if jid.User != "905551234567" {
		t.Errorf("User = %q, want %q", jid.User, "905551234567")
	}
	if jid.Server != "s.whatsapp.net" {
		t.Errorf("Server = %q, want %q", jid.Server, "s.whatsapp.net")
	}

	if _, err := FormatPhoneNumber("2025550134"); !errors.Is(err, ErrPhoneInvalid) {
		t.Errorf("error = %v, want %v", err, ErrPhoneInvalid)
	}
}
