package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/nyaruka/phonenumbers/v2"
)

// phoneDefaultRegion is the ISO 3166-1 alpha-2 region libphonenumber parses a
// local-format number against, for example "TR" or "ID". An empty value means
// every number must arrive in full international format with a leading "+".
//
// Unlike the other tunables it changes after startup, because an admin can set
// it from the settings page, so it is guarded and reached through PhoneRegion.
var (
	phoneRegionMu      sync.RWMutex
	phoneDefaultRegion string
)

// PhoneRegion returns the region the parser currently uses.
func PhoneRegion() string {
	phoneRegionMu.RLock()
	defer phoneRegionMu.RUnlock()
	return phoneDefaultRegion
}

// SetPhoneRegion replaces the region the parser uses. An unsupported code is
// refused, so a bad value cannot silently turn every national number invalid.
func SetPhoneRegion(region string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(region))
	if normalized != "" && !IsSupportedRegion(normalized) {
		return false
	}

	phoneRegionMu.Lock()
	defer phoneRegionMu.Unlock()
	phoneDefaultRegion = normalized
	return true
}

// IsSupportedRegion reports whether libphonenumber knows this region code.
func IsSupportedRegion(region string) bool {
	return phonenumbers.GetSupportedRegions()[strings.ToUpper(strings.TrimSpace(region))]
}

// SkipWhatsAppRegistrationCheck turns off the IsOnWhatsApp lookup before a send.
var SkipWhatsAppRegistrationCheck bool

// PhoneCountryCode is the deprecated calling-code form of PhoneDefaultRegion.
// It is read only by LoadPhoneConfig, which derives the region from it.
var PhoneCountryCode string

// LoadPhoneConfig reads the phone tunables once at startup. Both binaries call
// it, so the API and the worker always classify a number the same way.
func LoadPhoneConfig() {
	PhoneCountryCode = strings.TrimSpace(os.Getenv("PHONE_COUNTRY_CODE"))
	SetPhoneRegion(resolvePhoneRegion())
	SkipWhatsAppRegistrationCheck = resolveSkipRegistrationCheck()

	if PhoneRegion() == "" {
		log.Println("PHONE_DEFAULT_REGION is empty: every phone number must be in full international format with a leading +")
	}
}

// resolvePhoneRegion prefers PHONE_DEFAULT_REGION and falls back to deriving a
// region from the deprecated PHONE_COUNTRY_CODE calling code.
func resolvePhoneRegion() string {
	raw := strings.ToUpper(strings.TrimSpace(os.Getenv("PHONE_DEFAULT_REGION")))
	if raw != "" {
		if IsSupportedRegion(raw) {
			return raw
		}
		// raw comes from PHONE_DEFAULT_REGION, an operator-set environment
		// variable read once at startup, never from a request.
		// #nosec G706
		log.Printf("PHONE_DEFAULT_REGION=%q is not a supported region code; falling back to international-only numbers", raw)
		return ""
	}

	if PhoneCountryCode == "" {
		return ""
	}
	return regionFromCallingCode(PhoneCountryCode)
}

// regionFromCallingCode maps a legacy calling code such as "90" onto its main
// region code. A calling code shared by several countries resolves to the main
// one, which is why the deprecation message asks for an explicit region.
func regionFromCallingCode(callingCode string) string {
	cc, err := strconv.Atoi(callingCode)
	if err != nil {
		log.Printf("PHONE_COUNTRY_CODE=%q is not a number; falling back to international-only numbers", callingCode)
		return ""
	}

	region := phonenumbers.GetRegionCodeForCountryCode(cc)
	if region == "" || region == "ZZ" {
		log.Printf("PHONE_COUNTRY_CODE=%q maps to no region; falling back to international-only numbers", callingCode)
		return ""
	}

	log.Printf("DEPRECATED: PHONE_COUNTRY_CODE=%s derived PHONE_DEFAULT_REGION=%s; set PHONE_DEFAULT_REGION explicitly", callingCode, region)
	return region
}

// resolveSkipRegistrationCheck reads the current flag and falls back to the
// deprecated ALLOW_9_DIGIT_PHONE_NUMBER name.
func resolveSkipRegistrationCheck() bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("SKIP_WHATSAPP_REGISTRATION_CHECK")), "true") {
		return true
	}

	if strings.EqualFold(strings.TrimSpace(os.Getenv("ALLOW_9_DIGIT_PHONE_NUMBER")), "true") {
		log.Println("DEPRECATED: ALLOW_9_DIGIT_PHONE_NUMBER now means \"skip the IsOnWhatsApp check\"; rename it to SKIP_WHATSAPP_REGISTRATION_CHECK")
		return true
	}

	return false
}
