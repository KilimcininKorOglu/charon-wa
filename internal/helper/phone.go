package helper

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nyaruka/phonenumbers/v2"
	"go.mau.fi/whatsmeow/types"

	"charon/config"
)

// Sentinel errors returned by NormalizePhone. Callers should match them with
// errors.Is instead of comparing message text.
var (
	ErrPhoneEmpty       = errors.New("phone number is empty")
	ErrPhoneNoRegion    = errors.New("phone number has no country code and PHONE_DEFAULT_REGION is not set")
	ErrPhoneUnparseable = errors.New("phone number could not be parsed")
	ErrPhoneInvalid     = errors.New("phone number is not valid for its country")
)

// NormalizePhone converts any operator-typed phone number into its canonical
// bare E.164 form: digits only, no leading "+". For example "0555 123 45 67"
// with PHONE_DEFAULT_REGION=TR becomes "905551234567".
//
// A number that starts with "+" (or with the international prefix of the
// default region, such as "00") is parsed as an international number, so
// sending abroad works regardless of the configured region. A number without a
// country code is parsed as a national number of PHONE_DEFAULT_REGION; when
// that variable is empty, such a number is rejected with ErrPhoneNoRegion.
//
// Validity is decided by Google libphonenumber, per country, so every country's
// own numbering plan applies. FIXED_LINE numbers are accepted, because WhatsApp
// Business runs on landlines.
//
// The bare form (no "+") is deliberate: WhatsApp JIDs carry the number in
// exactly that shape, so a stored value compares equal to an incoming
// message's Sender.User.
func NormalizePhone(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ErrPhoneEmpty
	}

	region := config.PhoneRegion()
	if region == "" && !strings.HasPrefix(trimmed, "+") {
		return "", ErrPhoneNoRegion
	}

	num, err := phonenumbers.Parse(trimmed, region)
	if err == nil && phonenumbers.IsValidNumber(num) {
		return bareE164(num), nil
	}

	if retry, ok := parseAsInternational(trimmed, region); ok {
		return bareE164(retry), nil
	}

	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrPhoneUnparseable, err)
	}
	return "", ErrPhoneInvalid
}

// minBareInternationalLength is the shortest digit run that may be retried as a
// full international number. Below it the retry would start guessing: a short
// national number of the default region can accidentally read as a valid
// foreign one.
const minBareInternationalLength = 11

// parseAsInternational retries a digits-only number as a full international
// number, for the case where an operator (or an upstream system) typed a
// foreign number without its leading "+". It only runs after the national parse
// has already failed, so a number that is valid in the default region always
// wins.
func parseAsInternational(trimmed, region string) (*phonenumbers.PhoneNumber, bool) {
	if region == "" || len(trimmed) < minBareInternationalLength {
		return nil, false
	}
	for _, r := range trimmed {
		if r < '0' || r > '9' {
			return nil, false
		}
	}

	num, err := phonenumbers.Parse("+"+trimmed, "")
	if err != nil || !phonenumbers.IsValidNumber(num) {
		return nil, false
	}
	return num, true
}

func bareE164(num *phonenumbers.PhoneNumber) string {
	return strings.TrimPrefix(phonenumbers.Format(num, phonenumbers.E164), "+")
}

// FormatPhoneNumber normalises a phone number and wraps it in a WhatsApp user
// JID. It is the JID-shaped form of NormalizePhone.
func FormatPhoneNumber(phone string) (types.JID, error) {
	normalized, err := NormalizePhone(phone)
	if err != nil {
		return types.JID{}, err
	}

	return types.JID{
		User:   normalized,
		Server: types.DefaultUserServer,
	}, nil
}

// SkipWhatsAppRegistrationCheck reports whether the IsOnWhatsApp registration
// check should be skipped. It is a deployment-wide switch
// (SKIP_WHATSAPP_REGISTRATION_CHECK), not a per-number decision: now that every
// number is validated against its own country's numbering plan, the old
// per-number heuristic has nothing left to guess.
func SkipWhatsAppRegistrationCheck() bool {
	return config.SkipWhatsAppRegistrationCheck
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
