import { useCallback, useRef, useState } from 'react'

/** One server snapshot as a view shows it. `data` is only ever a server response. */
export interface Snapshot<T> {
  data: T | null
  loading: boolean
  error: unknown
}

const emptySnapshot = { data: null, loading: false, error: null }

export interface LoadOptions {
  /** Keep showing the previous data while reloading (a refresh of the same selection). */
  keepData?: boolean
}

/**
 * One REST snapshot (accounts, orders, book, executions, or a portfolio) and
 * its loader.
 *
 * Starting a load aborts the previous one, and only the latest load may write
 * state: a response that arrives after the user switched account or symbol, or
 * after a newer refresh, is dropped even if the fetch ignored the abort. A
 * failed load clears the data, so an old value is never shown as current.
 */
export function useSnapshot<T>() {
  const [snapshot, setSnapshot] = useState<Snapshot<T>>(emptySnapshot)
  const latest = useRef<AbortController | null>(null)

  const load = useCallback(
    async (fetcher: (signal: AbortSignal) => Promise<T>, { keepData = false }: LoadOptions = {}): Promise<T | undefined> => {
      latest.current?.abort()
      const controller = new AbortController()
      latest.current = controller
      setSnapshot((previous) => ({ data: keepData ? previous.data : null, loading: true, error: null }))
      try {
        const data = await fetcher(controller.signal)
        if (latest.current !== controller) return undefined // stale
        setSnapshot({ data, loading: false, error: null })
        return data
      } catch (error) {
        if (latest.current !== controller) return undefined // stale
        // Drop old values on failure rather than show them as current.
        setSnapshot({ data: null, loading: false, error })
        return undefined
      }
    },
    [],
  )

  const cancel = useCallback(() => {
    latest.current?.abort()
    latest.current = null
  }, [])

  return [snapshot, load, cancel] as const
}
