import { useEffect, useMemo, useRef, useState } from "react"
import { ChevronDown } from "lucide-react"
import type { CountryCode } from "libphonenumber-js/max"
import {
  countryOptions,
  examplePlaceholder,
  formatAsYouType,
  parseFlexible,
  useDefaultRegion,
} from "../../lib/phone"

export interface PhoneMeta {
  isValid: boolean
  /** Bare E.164 digits, empty while the number is invalid. */
  digits: string
  /** "+"-prefixed E.164, for display only. */
  e164: string
  country?: CountryCode
  national: string
}

interface PhoneInputProps {
  value: string
  onChange: (value: string, meta: PhoneMeta) => void
  label?: string
  error?: string
  placeholder?: string
  disabled?: boolean
  required?: boolean
  id?: string
  className?: string
  size?: "sm" | "md"
  defaultCountry?: CountryCode
}

const SIZE_CLASSES: Record<"sm" | "md", string> = {
  sm: "px-2 py-1.5 text-xs",
  md: "px-3 py-2 text-sm",
}

const BASE_INPUT =
  "w-full bg-bg-input border border-border text-cyber-green placeholder-cyber-green-muted/50 font-mono focus:outline-none focus:border-cyber-green/50 transition-all"

/**
 * Phone number field with a country selector and live validation.
 *
 * The value it emits is bare E.164 digits, and it emits an EMPTY STRING while
 * the number is not valid. That is deliberate, not a bug: every call site
 * already guards its submit button on a truthy value, so an invalid number
 * disables the action for free, and no half-typed number ever reaches a request
 * body or a URL segment. Do not "fix" it to emit the raw text.
 */
export function PhoneInput({
  value,
  onChange,
  label,
  error,
  placeholder,
  disabled,
  required,
  id,
  className = "",
  size = "md",
  defaultCountry,
}: PhoneInputProps) {
  const backendRegion = useDefaultRegion()
  const initialRegion = defaultCountry ?? backendRegion

  const [country, setCountry] = useState<CountryCode>(initialRegion)
  const [draft, setDraft] = useState("")
  const [touched, setTouched] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const countries = useMemo(() => countryOptions(), [])

  const parsed = parseFlexible(draft, country)

  // The parent may reset or preset the value. Reseeding on every render would
  // erase the box on each invalid keystroke, so the draft is only replaced when
  // the incoming value is not what this draft already means.
  useEffect(() => {
    if (value === parsed.digits) return
    setDraft(value === "" ? "" : value)
    if (value === "") setTouched(false)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value])

  // Follow the backend region until the operator picks a country themselves.
  useEffect(() => {
    if (defaultCountry || draft !== "") return
    setCountry(backendRegion)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [backendRegion])

  function emit(nextDraft: string, nextCountry: CountryCode) {
    const next = parseFlexible(nextDraft, nextCountry)
    onChange(next.digits, {
      isValid: next.isValid,
      digits: next.digits,
      e164: next.e164,
      country: next.country,
      national: next.national,
    })
  }

  function handleInput(raw: string) {
    const detected = parseFlexible(raw, country)
    // A pasted international number belongs to whatever country it names, so the
    // selector follows it instead of fighting the paste.
    const nextCountry = detected.country && detected.country !== country ? detected.country : country
    if (nextCountry !== country) setCountry(nextCountry)

    setDraft(liveFormat(raw, nextCountry, inputRef.current))
    emit(raw, nextCountry)
  }

  function handleCountryChange(next: CountryCode) {
    setCountry(next)
    // Reparse what is already typed in the new region instead of clearing it.
    emit(draft, next)
  }

  const inputId = id || label?.toLowerCase().replace(/\s+/g, "-")
  const errorId = inputId ? `${inputId}-error` : undefined
  const showInvalid = touched && draft !== "" && !parsed.isValid
  const message = error || (showInvalid ? "Not a valid number for the selected country" : "")

  return (
    <div className={`flex flex-col gap-1.5 ${className}`}>
      {label && (
        <label htmlFor={inputId} className="text-xs text-cyber-green-dim uppercase tracking-wider">
          {label}
        </label>
      )}

      <div className="flex gap-1.5">
        <div className="relative shrink-0">
          <select
            value={country}
            onChange={(e) => handleCountryChange(e.target.value as CountryCode)}
            disabled={disabled}
            aria-label="Country"
            autoComplete="tel-country-code"
            className={`${BASE_INPUT} ${SIZE_CLASSES[size]} pr-6 appearance-none cursor-pointer w-28`}
          >
            {countries.map((option) => (
              <option key={option.code} value={option.code}>
                {option.code} +{option.callingCode}
              </option>
            ))}
          </select>
          <ChevronDown
            size={12}
            className="absolute right-2 top-1/2 -translate-y-1/2 text-cyber-green-muted pointer-events-none"
          />
        </div>

        <input
          ref={inputRef}
          id={inputId}
          type="tel"
          inputMode="tel"
          autoComplete="tel-national"
          value={draft}
          disabled={disabled}
          required={required}
          aria-invalid={message !== ""}
          aria-describedby={message !== "" ? errorId : undefined}
          placeholder={placeholder ?? examplePlaceholder(country)}
          onChange={(e) => handleInput(e.target.value)}
          onBlur={() => {
            setTouched(true)
            if (parsed.isValid) setDraft(parsed.national)
          }}
          className={`${BASE_INPUT} ${SIZE_CLASSES[size]} flex-1 min-w-0 ${message !== "" ? "border-cyber-danger/50" : ""}`}
        />
      </div>

      {message !== "" && (
        <span id={errorId} role="alert" className="text-xs text-cyber-danger">
          {message}
        </span>
      )}
    </div>
  )
}

/**
 * Formats while typing, but only when the caret sits at the end of the text.
 * Reformatting mid-edit moves the caret and makes the field fight the operator,
 * so an edit in the middle keeps the raw text and is formatted on blur.
 */
function liveFormat(raw: string, region: CountryCode, element: HTMLInputElement | null): string {
  const caretAtEnd = element === null || element.selectionStart === raw.length
  if (!caretAtEnd) return raw
  return formatAsYouType(raw, region)
}
