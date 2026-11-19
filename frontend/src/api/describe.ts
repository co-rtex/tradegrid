import { ApiError } from './client'

/**
 * Completes "Could not load …: " for any failure from the REST client, in
 * words a demo user can act on. It never reports a failure as data.
 */
export function describeLoadError(error: unknown): string {
  if (error instanceof ApiError) {
    switch (error.kind) {
      case 'http':
        return `${error.message} (${error.code})`
      case 'network':
        return 'the server could not be reached'
      case 'timeout':
        return 'the server did not respond in time'
      case 'invalid-response':
        return error.status !== null && error.status >= 500
          ? `the server is unavailable (HTTP ${error.status})`
          : `the server sent an unexpected response (HTTP ${error.status ?? 'unknown'})`
      case 'aborted':
        return 'the request was cancelled'
    }
  }
  return 'an unexpected error occurred'
}
