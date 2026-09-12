package config

import "testing"

func TestLoadPhoneConfigRegion(t *testing.T) {
	tests := []struct {
		name        string
		region      string
		countryCode string
		want        string
	}{
		{name: "explicit region", region: "TR", want: "TR"},
		{name: "explicit region is upper-cased", region: "tr", want: "TR"},
		{name: "explicit region wins over the legacy code", region: "ID", countryCode: "90", want: "ID"},
		{name: "legacy 90 derives Turkey", countryCode: "90", want: "TR"},
		{name: "legacy 62 derives Indonesia", countryCode: "62", want: "ID"},
		{name: "legacy 1 derives the United States", countryCode: "1", want: "US"},
		{name: "unsupported region falls back to empty", region: "XX", want: ""},
		{name: "non-numeric legacy code falls back to empty", countryCode: "abc", want: ""},
		{name: "unassigned legacy code falls back to empty", countryCode: "999", want: ""},
		{name: "nothing set", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PHONE_DEFAULT_REGION", tt.region)
			t.Setenv("PHONE_COUNTRY_CODE", tt.countryCode)

			LoadPhoneConfig()

			if PhoneDefaultRegion != tt.want {
				t.Errorf("PhoneDefaultRegion = %q, want %q", PhoneDefaultRegion, tt.want)
			}
		})
	}
}

func TestLoadPhoneConfigSkipRegistrationCheck(t *testing.T) {
	tests := []struct {
		name    string
		current string
		legacy  string
		want    bool
	}{
		{name: "current flag set", current: "true", want: true},
		{name: "legacy flag set", legacy: "true", want: true},
		{name: "current flag wins when both are set", current: "true", legacy: "false", want: true},
		{name: "legacy flag still applies when the current one is false", current: "false", legacy: "true", want: true},
		{name: "neither set", want: false},
		{name: "both false", current: "false", legacy: "false", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SKIP_WHATSAPP_REGISTRATION_CHECK", tt.current)
			t.Setenv("ALLOW_9_DIGIT_PHONE_NUMBER", tt.legacy)

			LoadPhoneConfig()

			if SkipWhatsAppRegistrationCheck != tt.want {
				t.Errorf("SkipWhatsAppRegistrationCheck = %v, want %v", SkipWhatsAppRegistrationCheck, tt.want)
			}
		})
	}
}
