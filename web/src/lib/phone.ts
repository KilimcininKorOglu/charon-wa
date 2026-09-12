import { useEffect, useState } from "react"
import {
  AsYouType,
  getCountries,
  getCountryCallingCode,
  getExampleNumber,
  parsePhoneNumberFromString,
  type CountryCode,
} from "libphonenumber-js/max"
import examples from "libphonenumber-js/mobile/examples"
import api from "./api"

/**
 * Used until `GET /api/system/phone-config` answers, and kept if it never does.
 * The app must stay usable when the endpoint is missing, so this is a fallback,
 * not a configuration value.
 */
export const FALLBACK_REGION: CountryCode = "TR"

/**
 * Shortest bare digit run that may be read as a full international number.
 * Mirrors minBareInternationalLength in internal/helper/phone.go; below it a
 * national number can read as a valid foreign one by accident.
 */
const MIN_BARE_INTERNATIONAL_LENGTH = 11

/** Result of interpreting whatever the operator typed. */
export interface ParsedPhone {
  /** Bare E.164 digits with no leading "+", empty when the number is invalid. */
  digits: string
  /** "+"-prefixed E.164, for display only. Empty when the number is invalid. */
  e164: string
  /** The country the number belongs to, when it could be determined. */
  country?: CountryCode
  /** National format, for display only. */
  national: string
  isValid: boolean
}

const EMPTY_PARSE: ParsedPhone = { digits: "", e164: "", national: "", isValid: false }

function isAllDigits(value: string): boolean {
  return value.length > 0 && /^\d+$/.test(value)
}

/**
 * Interprets an operator-typed number, trying the same things the backend does
 * and in the same order.
 *
 * 1. A "+" or "00" prefix is an international number, whatever the region is.
 * 2. Otherwise it is a national number of `region`.
 * 3. A bare digit run of at least 11 digits that is not valid nationally is
 *    retried as an international number, so "628123456789" still works in a
 *    Turkish deployment.
 */
export function parseFlexible(raw: string, region: CountryCode): ParsedPhone {
  const trimmed = raw.trim()
  if (trimmed === "") return EMPTY_PARSE

  const national = parsePhoneNumberFromString(trimmed, region)
  if (national?.isValid()) return toParsedPhone(national)

  if (isAllDigits(trimmed) && trimmed.length >= MIN_BARE_INTERNATIONAL_LENGTH) {
    const international = parsePhoneNumberFromString(`+${trimmed}`)
    if (international?.isValid()) return toParsedPhone(international)
  }

  return EMPTY_PARSE
}

function toParsedPhone(parsed: ReturnType<typeof parsePhoneNumberFromString>): ParsedPhone {
  if (!parsed) return EMPTY_PARSE
  return {
    digits: parsed.number.replace(/^\+/, ""),
    e164: parsed.number,
    country: parsed.country,
    national: parsed.formatNational(),
    isValid: true,
  }
}

/**
 * Canonical value to send to the API: bare E.164 digits, or an empty string when
 * the number is not valid. Every request body and URL segment uses this shape,
 * because that is what the backend stores and compares against.
 */
export function toCanonicalDigits(raw: string, region: CountryCode): string {
  return parseFlexible(raw, region).digits
}

/**
 * Human-readable form of a stored value, for read-only views only.
 *
 * Falls back to the raw string whenever it cannot be parsed, which is the normal
 * case for a LID, a group id, or free-form operator text. Never apply it to a
 * form value, a request body or a URL segment.
 */
export function formatPhoneDisplay(raw: string): string {
  const trimmed = (raw ?? "").trim()
  if (trimmed === "") return raw

  const parsed = parsePhoneNumberFromString(trimmed.startsWith("+") ? trimmed : `+${trimmed}`)
  if (!parsed?.isValid()) return raw

  return parsed.formatInternational()
}

/** Live formatting while typing, in the given region. */
export function formatAsYouType(raw: string, region: CountryCode): string {
  return new AsYouType(region).input(raw)
}

/** Placeholder for the selected country, so the expected shape is visible. */
export function examplePlaceholder(region: CountryCode): string {
  return getExampleNumber(region, examples)?.formatNational() ?? ""
}

const displayNames =
  typeof Intl !== "undefined" && "DisplayNames" in Intl
    ? new Intl.DisplayNames(undefined, { type: "region" })
    : undefined

export interface CountryOption {
  code: CountryCode
  callingCode: string
  label: string
}

/** Every supported country, sorted by its localised name. */
export function countryOptions(): CountryOption[] {
  return getCountries()
    .map((code) => ({
      code,
      callingCode: getCountryCallingCode(code),
      label: displayNames?.of(code) ?? code,
    }))
    .sort((a, b) => a.label.localeCompare(b.label))
}

/**
 * The backend's default region, fetched once per page load. The promise is
 * memoised at module scope, so mounting several PhoneInputs costs one request.
 */
let regionPromise: Promise<CountryCode> | undefined

function fetchDefaultRegion(): Promise<CountryCode> {
  if (!regionPromise) {
    regionPromise = api
      .get("/api/system/phone-config")
      .then((res) => {
        const region = res.data?.data?.defaultRegion
        return typeof region === "string" && region !== "" ? (region as CountryCode) : FALLBACK_REGION
      })
      .catch(() => FALLBACK_REGION)
  }
  return regionPromise
}

/** Reads the backend's default region, falling back while it loads. */
export function useDefaultRegion(): CountryCode {
  const [region, setRegion] = useState<CountryCode>(FALLBACK_REGION)

  useEffect(() => {
    let active = true
    void fetchDefaultRegion().then((value) => {
      if (active) setRegion(value)
    })
    return () => {
      active = false
    }
  }, [])

  return region
}
