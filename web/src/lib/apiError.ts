import axios from "axios"
import type { ApiResponse } from "./types"

export interface ApiFailure {
  /** Machine-readable code from the server envelope, empty when absent. */
  code: string
  /** Message to show the operator. Never empty. */
  message: string
}

/**
 * Reads the server's error envelope out of a rejected request.
 *
 * Every handler answers with { success, message, error: { code, details } }, but
 * call sites used to discard it with `catch { toast.error("...") }` and show
 * their own generic text instead. The server already said what went wrong.
 *
 * `details` is deliberately ignored: the backend logs it server-side only.
 */
export function apiFailure(err: unknown, fallback: string): ApiFailure {
  if (axios.isAxiosError<ApiResponse>(err)) {
    const body = err.response?.data
    return {
      code: body?.error?.code ?? "",
      message: body?.message || fallback,
    }
  }
  return { code: "", message: fallback }
}

/** True when the failure is the server rejecting the phone number itself. */
export function isInvalidPhone(failure: ApiFailure): boolean {
  return failure.code === "INVALID_PHONE"
}
