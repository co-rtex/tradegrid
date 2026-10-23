// Global setup for Vitest. Loaded only by vitest.config.ts, never by the app.
import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

// Unmount rendered components between tests so each test starts from an
// empty document.
afterEach(() => {
  cleanup()
})
