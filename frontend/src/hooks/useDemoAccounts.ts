import { useCallback, useEffect, useState } from 'react'
import type { TradeGridClient } from '../api/client'

/** A selectable demo account. `userId` is a demo selector, not a sign-in. */
export interface DemoAccount {
  userId: string
  displayName: string
}

export type DemoAccountDirectory =
  | { status: 'loading' }
  | { status: 'ready'; accounts: DemoAccount[] }
  | { status: 'error'; error: unknown }

/**
 * Loads the demo accounts once from `GET /api/v1/accounts`, for the app-level
 * account selector. The account set never changes while the server runs, so
 * only identities are kept here; views read balances themselves. `reload`
 * retries after a failure.
 */
export function useDemoAccounts(client: TradeGridClient): [DemoAccountDirectory, () => void] {
  const [directory, setDirectory] = useState<DemoAccountDirectory>({ status: 'loading' })
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    const controller = new AbortController()
    client.getAccounts({ signal: controller.signal }).then(
      (accounts) => {
        if (controller.signal.aborted) return
        setDirectory({
          status: 'ready',
          accounts: accounts.map(({ userId, displayName }) => ({ userId, displayName })),
        })
      },
      (error: unknown) => {
        if (controller.signal.aborted) return
        setDirectory({ status: 'error', error })
      },
    )
    return () => controller.abort()
  }, [client, attempt])

  const reload = useCallback(() => {
    setDirectory({ status: 'loading' })
    setAttempt((n) => n + 1)
  }, [])

  return [directory, reload]
}

/** sessionStorage key for this tab's selected demo account. */
export const SELECTED_ACCOUNT_KEY = 'tradegrid.selectedAccount'

function readStored(): string | null {
  try {
    return window.sessionStorage.getItem(SELECTED_ACCOUNT_KEY)
  } catch {
    return null // storage disabled: the selection simply is not remembered
  }
}

function writeStored(userId: string) {
  try {
    window.sessionStorage.setItem(SELECTED_ACCOUNT_KEY, userId)
  } catch {
    // Not remembered across reloads; the selection itself still works.
  }
}

/**
 * The app-level selected demo account. Each browser tab keeps its own choice
 * (sessionStorage), so one window can stay on Alice and another on Bob across
 * reloads. Until a known account is chosen, the first account in the
 * directory is selected and shown in the selector; a remembered ID that the
 * server no longer has is ignored. Returns `null` while no directory is loaded.
 */
export function useSelectedAccount(accounts: DemoAccount[] | null): [string | null, (userId: string) => void] {
  const [chosen, setChosen] = useState<string | null>(readStored)

  const select = useCallback((userId: string) => {
    setChosen(userId)
    writeStored(userId)
  }, [])

  if (accounts === null) return [null, select]
  const known = accounts.some((account) => account.userId === chosen)
  return [known ? chosen : (accounts[0]?.userId ?? null), select]
}
