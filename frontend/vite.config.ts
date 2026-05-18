import { defineConfig, loadEnv } from 'vite'
import react from '@vitejs/plugin-react'

function normalizeBasePath(value: string | undefined): string {
  const raw = (value || '/').trim()
  if (!raw || raw === '/') {
    return '/'
  }
  return `/${raw.replace(/^\/+|\/+$/g, '')}/`
}

function joinBasePath(basePath: string, suffix: string): string {
  if (basePath === '/') {
    return suffix
  }
  return `${basePath}${suffix.replace(/^\/+/, '')}`
}

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const basePath = normalizeBasePath(env.VITE_APP_BASE_PATH)

  return {
    base: basePath,
    plugins: [react()],
    server: {
      host: '127.0.0.1',
      port: 5173,
      proxy: {
        [joinBasePath(basePath, '/api')]: {
          target: 'http://127.0.0.1:5001',
          changeOrigin: true,
        },
        [joinBasePath(basePath, '/v1')]: {
          target: 'http://127.0.0.1:5001',
          changeOrigin: true,
        },
      },
    },
  }
})
