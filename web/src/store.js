import { reactive } from 'vue'

export const store = reactive({ me: null, loading: true, failed: false })

export async function loadMe() {
  try {
    const r = await fetch('/api/me', { credentials: 'same-origin' })
    store.me = await r.json()
    store.failed = false
  } catch {
    store.failed = true
  } finally {
    store.loading = false
  }
}

export async function api(method, path, body) {
  const r = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  })
  let data = null
  try { data = await r.json() } catch { /* tubuh kosong */ }
  return { ok: r.ok, status: r.status, data }
}

export async function logout() {
  await api('POST', '/auth/logout')
  store.me = null
  location.href = '/'
}
