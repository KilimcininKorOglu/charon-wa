import { useMemo } from "react"
import { ChevronDown } from "lucide-react"
import type { CountryCode } from "libphonenumber-js/max"
import { countryOptions } from "../../lib/phone"

interface CountrySelectProps {
  value: string
  onChange: (value: string) => void
  label?: string
  /** Text of the empty option. Selecting it clears the value. */
  emptyLabel: string
  disabled?: boolean
  id?: string
  className?: string
}

/**
 * Country picker for the two region settings. It lists the same countries as
 * PhoneInput but carries an empty option, because both settings accept "not
 * set": a user then follows the system default, and the system then requires a
 * country code on every number.
 */
export function CountrySelect({
  value,
  onChange,
  label,
  emptyLabel,
  disabled,
  id,
  className = "",
}: CountrySelectProps) {
  const countries = useMemo(() => countryOptions(), [])
  const selectId = id || label?.toLowerCase().replace(/\s+/g, "-")

  return (
    <div className={`flex flex-col gap-1.5 ${className}`}>
      {label && (
        <label htmlFor={selectId} className="text-xs text-cyber-green-dim uppercase tracking-wider">
          {label}
        </label>
      )}
      <div className="relative">
        <select
          id={selectId}
          value={value}
          disabled={disabled}
          onChange={(e) => onChange(e.target.value)}
          className="w-full bg-bg-input border border-border text-cyber-green px-3 py-2 text-sm font-mono focus:outline-none focus:border-cyber-green/50 appearance-none cursor-pointer"
        >
          <option value="">{emptyLabel}</option>
          {countries.map((option: { code: CountryCode; callingCode: string; label: string }) => (
            <option key={option.code} value={option.code}>
              {option.label} ({option.code} +{option.callingCode})
            </option>
          ))}
        </select>
        <ChevronDown
          size={14}
          className="absolute right-2 top-1/2 -translate-y-1/2 text-cyber-green-muted pointer-events-none"
        />
      </div>
    </div>
  )
}
