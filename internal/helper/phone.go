package helper

import (
	"fmt"
	"regexp"
	"strings"

	"go.mau.fi/whatsmeow/types"

	"charon/config"
)

// FormatPhoneNumber converts a phone number string to a WhatsApp JID.
//
// If PHONE_COUNTRY_CODE is set (e.g. "90" for Turkey, "62" for Indonesia):
//   - "0XXXXXXXXXX"  → "{cc}XXXXXXXXXX"  (strip leading 0, prepend country code)
//   - "XXXXXXXXXX"   → "{cc}XXXXXXXXXX"  (no cc prefix detected, prepend country code)
//   - "{cc}XXXXXXXX" → used as-is
//
// If PHONE_COUNTRY_CODE is empty, the number must already be in full international
// format (E.164 without the +, e.g. "905551234567").
//
// Final length must be 7–15 digits per ITU-T E.164.
func FormatPhoneNumber(phone string) (types.JID, error) {
	// Only accept digits, +, -, (, ), and spaces
	validFormat := regexp.MustCompile(`^[\d\s\+\-\(\)]+$`)
	if !validFormat.MatchString(phone) {
		return types.JID{}, fmt.Errorf("invalid phone number format: contains invalid characters")
	}

	// Strip everything except digits
	cleaned := regexp.MustCompile(`[^\d]`).ReplaceAllString(phone, "")

	if len(cleaned) < 7 {
		return types.JID{}, fmt.Errorf("phone number too short")
	}

	cc := config.PhoneCountryCode

	if cc != "" {
		// Auto-convert: "0XXXXXXXXX" → "{cc}XXXXXXXXX"
		if strings.HasPrefix(cleaned, "0") {
			cleaned = cc + cleaned[1:]
		} else if !strings.HasPrefix(cleaned, cc) {
			// Local format without leading 0 and without country code → prepend cc
			cleaned = cc + cleaned
		}

		if !strings.HasPrefix(cleaned, cc) {
			return types.JID{}, fmt.Errorf("phone number must start with %s (country code). Example: %sXXXXXXXXXX", cc, cc)
		}
	}

	// E.164 length: 7–15 digits (country code included)
	if len(cleaned) < 7 || len(cleaned) > 15 {
		return types.JID{}, fmt.Errorf("invalid phone number length (must be 7–15 digits in E.164 format)")
	}

	return types.JID{
		User:   cleaned,
		Server: types.DefaultUserServer,
	}, nil
}

// ShouldSkipValidation reports whether the IsOnWhatsApp registration check should
// be skipped for this phone number. Skipping is only possible when the
// ALLOW_9_DIGIT_PHONE_NUMBER environment variable is set to "true".
//
// Numbers that trigger skipping:
//   - Numbers with a leading 0 (local format, may not resolve correctly)
//   - Numbers that don't start with the configured country code (local format without cc)
//   - Numbers shorter than 10 digits
func ShouldSkipValidation(phone string) bool {
	if !config.Allow9DigitPhoneNumber {
		return false
	}

	cleaned := regexp.MustCompile(`[^\d]`).ReplaceAllString(phone, "")

	// Local format with leading 0
	if strings.HasPrefix(cleaned, "0") {
		return true
	}

	// Local format without country code prefix
	cc := config.PhoneCountryCode
	if cc != "" && !strings.HasPrefix(cleaned, cc) {
		return true
	}

	// Genuinely short number
	if len(cleaned) < 10 {
		return true
	}

	return false
}

// ExtractPhoneFromJID extracts the phone number from a WhatsApp JID string.
// "905123456789:43@s.whatsapp.net" → "905123456789"
// "905551234567@s.whatsapp.net"     → "905551234567"
func ExtractPhoneFromJID(jid string) string {
	atSplit := strings.SplitN(jid, "@", 2)
	if len(atSplit) == 0 {
		return jid
	}
	beforeAt := atSplit[0]
	colonSplit := strings.SplitN(beforeAt, ":", 2)
	return colonSplit[0]
}
