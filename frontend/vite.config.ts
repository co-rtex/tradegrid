import react from '@vitejs/plugin-react'
import { defineConfig, type ProxyOptions } from 'vite'

// The browser calls same-origin /api/v1/...; the dev and preview servers
// forward /api to the Go backend, which listens only on 127.0.0.1. Its port
// follows the backend's own TRADEGRID_HTTP_PORT variable (default 8080), so
// one setting configures both servers.
const backendPort = process.env.TRADEGRID_HTTP_PORT?.trim() || '8080'

if (!/^\d{1,5}$/.test(backendPort) || Number(backendPort) < 1 || Number(backendPort) > 65535) {
  throw new Error(`TRADEGRID_HTTP_PORT must be a port number from 1 to 65535, got "${backendPort}"`)
}

// No custom error handler: when the backend is down the proxy answers with an
// empty, non-JSON 502, which the client reports as "server unavailable" (and, for a
// POST, as "outcome unknown") rather than as a contracted rejection.
const proxy: Record<string, ProxyOptions> = {
  '/api': { target: `http://127.0.0.1:${backendPort}` },
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  server: { proxy },
  preview: { proxy },
})
