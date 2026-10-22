import { defineConfig, mergeConfig } from 'vitest/config'
import viteConfig from './vite.config.ts'

// Test-only configuration. It lives apart from vite.config.ts so `vite build`
// never loads Vitest, jsdom, or the test setup.
export default mergeConfig(
  viteConfig,
  defineConfig({
    test: {
      environment: 'jsdom',
      setupFiles: ['./src/test/setup.ts'],
      include: ['src/**/*.test.{ts,tsx}'],
      // Fail rather than pass silently if a filter or typo matches nothing.
      passWithNoTests: false,
    },
  }),
)
