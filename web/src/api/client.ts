/**
 * Single typed API client for the Orxest REST API.
 *
 * The backend always answers with JSON and returns errors shaped as
 * `{ error: { code, message, field? } }` with a 4xx/5xx status. Non-2xx
 * responses are turned into a typed `ApiError` so callers can surface
 * `error.message` directly in toasts and inline alerts.
 */

export const API_BASE = `${import.meta.env.VITE_API_BASE_URL ?? ''}/api`

export interface ApiErrorDetail {
  code: string
  message: string
  field?: string
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly field?: string

  constructor(status: number, code: string, message: string, field?: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.field = field
  }
}

/** Human readable message for any thrown value. */
export function errorMessage(error: unknown): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) return error.message
  if (typeof error === 'string') return error
  return 'Unexpected error'
}

export type QueryValue =
  | string
  | number
  | boolean
  | null
  | undefined
  | Array<string | number>

export function buildQuery(query?: Record<string, QueryValue>): string {
  if (!query) return ''
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === '') continue
    if (Array.isArray(value)) {
      if (value.length === 0) continue
      params.set(key, value.join(','))
      continue
    }
    params.set(key, String(value))
  }
  const encoded = params.toString()
  return encoded ? `?${encoded}` : ''
}

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE'
  /** JSON body. Mutually exclusive with `rawBody`. */
  body?: unknown
  /** Raw string body (used for YAML configuration uploads). */
  rawBody?: { contentType: string; body: string }
  query?: Record<string, QueryValue>
  signal?: AbortSignal
  headers?: Record<string, string>
  /** Skip JSON parsing and return the raw text (used for diagnostics). */
  asText?: boolean
}

async function parseError(response: Response): Promise<ApiError> {
  let code = 'http_error'
  let message = `${response.status} ${response.statusText}`
  let field: string | undefined
  try {
    const text = await response.text()
    if (text) {
      const parsed = JSON.parse(text) as { error?: ApiErrorDetail }
      if (parsed && typeof parsed === 'object' && parsed.error) {
        code = parsed.error.code ?? code
        message = parsed.error.message ?? message
        field = parsed.error.field
      } else {
        message = text
      }
    }
  } catch {
    // Non JSON error bodies keep the status line message.
  }
  return new ApiError(response.status, code, message, field)
}

export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { method = 'GET', body, rawBody, query, signal, headers, asText } = options
  const url = `${API_BASE}${path}${buildQuery(query)}`

  const init: RequestInit = {
    method,
    signal,
    headers: {
      Accept: 'application/json',
      ...headers,
    },
  }

  if (rawBody) {
    init.body = rawBody.body
    init.headers = { ...init.headers, 'Content-Type': rawBody.contentType }
  } else if (body !== undefined) {
    init.body = JSON.stringify(body)
    init.headers = { ...init.headers, 'Content-Type': 'application/json' }
  }

  let response: Response
  try {
    response = await fetch(url, init)
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === 'AbortError') throw cause
    throw new ApiError(0, 'network_error', 'Cannot reach the Orxest API. Is the server running?')
  }

  if (!response.ok) {
    throw await parseError(response)
  }

  if (response.status === 204) {
    return undefined as T
  }

  if (asText) {
    return (await response.text()) as unknown as T
  }

  const text = await response.text()
  if (!text) return undefined as T
  try {
    return JSON.parse(text) as T
  } catch {
    return text as unknown as T
  }
}

/** Normalises `{items,total}` envelopes (and bare arrays) into an array. */
export function unwrapItems<T>(value: ListLike<T>): T[] {
  if (Array.isArray(value)) return value
  if (value && Array.isArray((value as { items?: T[] }).items)) {
    return (value as { items: T[] }).items
  }
  return []
}

export type ListLike<T> = { items: T[]; total: number } | T[] | null | undefined
