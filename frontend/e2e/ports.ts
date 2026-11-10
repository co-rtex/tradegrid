// Ports reserved for the browser smoke tests, so they never touch a backend
// or frontend you already have running on 8080 / 5173.
export const backendPort = process.env.TRADEGRID_E2E_BACKEND_PORT ?? '18080'
export const frontendPort = process.env.TRADEGRID_E2E_FRONTEND_PORT ?? '15173'
export const backendURL = `http://127.0.0.1:${backendPort}`
