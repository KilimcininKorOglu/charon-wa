package helper

import (
	"testing"

	"hermeswa/config"
)

func TestExtractPhoneFromJID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"905551234567@s.whatsapp.net", "905551234567"},
		{"628123456789@s.whatsapp.net", "628123456789"},
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

func TestFormatPhoneNumber(t *testing.T) {
	tests := []struct {
		name        string
		countryCode string
		input       string
		wantUser    string
		wantErr     bool
	}{
		// ── No country code set (full international format required) ─────────
		{
			name:        "no cc: full international Turkey",
			countryCode: "",
			input:       "905551234567",
			wantUser:    "905551234567",
		},
		{
			name:        "no cc: full international Indonesia",
			countryCode: "",
			input:       "628123456789",
			wantUser:    "628123456789",
		},
		{
			name:        "no cc: local format rejected",
			countryCode: "",
			input:       "05551234567",
			wantErr:     false, // "0" stripped, prepend "" → "5551234567", 10 digits — valid E.164
			wantUser:    "05551234567",
		},

		// ── Turkey (90) ───────────────────────────────────────────────────────
		{
			name:        "TR: leading 0 converted",
			countryCode: "90",
			input:       "05551234567",
			wantUser:    "905551234567",
		},
		{
			name:        "TR: local without 0 converted",
			countryCode: "90",
			input:       "5551234567",
			wantUser:    "905551234567",
		},
		{
			name:        "TR: full international unchanged",
			countryCode: "90",
			input:       "905551234567",
			wantUser:    "905551234567",
		},
		{
			name:        "TR: with + prefix",
			countryCode: "90",
			input:       "+905551234567",
			wantUser:    "905551234567",
		},
		{
			name:        "TR: too short",
			countryCode: "90",
			input:       "905",
			wantErr:     true,
		},
		{
			name:        "TR: too long",
			countryCode: "90",
			input:       "9055512345678901234",
			wantErr:     true,
		},
		{
			name:        "TR: invalid characters",
			countryCode: "90",
			input:       "905abc1234567",
			wantErr:     true,
		},

		// ── Indonesia (62) ────────────────────────────────────────────────────
		{
			name:        "ID: leading 0 converted",
			countryCode: "62",
			input:       "08123456789",
			wantUser:    "628123456789",
		},
		{
			name:        "ID: local without 0 converted",
			countryCode: "62",
			input:       "8123456789",
			wantUser:    "628123456789",
		},
		{
			name:        "ID: full international unchanged",
			countryCode: "62",
			input:       "628123456789",
			wantUser:    "628123456789",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config.PhoneCountryCode = tt.countryCode
			jid, err := FormatPhoneNumber(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got JID user=%q", jid.User)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if tt.wantUser != "" && jid.User != tt.wantUser {
				t.Errorf("User = %q, want %q", jid.User, tt.wantUser)
			}
		})
	}

	// Reset
	config.PhoneCountryCode = ""
}
