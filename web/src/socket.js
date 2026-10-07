// Klien WebSocket: sambung ulang otomatis dengan jeda bertahap, kecuali diputus server dengan alasan tetap.
export function connectSocket({ onEvent, onStatus }) {
  let ws = null
  let closed = false
  let attempt = 0
  let timer = null
  const FINAL = new Set(['banned', 'replaced'])

  function open() {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    ws = new WebSocket(`${proto}://${location.host}/ws`)
    ws.onopen = () => { attempt = 0; onStatus('open') }
    ws.onmessage = (m) => {
      let e
      try { e = JSON.parse(m.data) } catch { return }
      if (e.t === 'error' && FINAL.has(e.error)) closed = true
      onEvent(e)
    }
    ws.onclose = () => {
      onStatus('closed')
      if (closed) return
      attempt++
      timer = setTimeout(open, Math.min(1000 * 2 ** attempt, 15000))
    }
  }
  open()

  return {
    send(msg) { if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify(msg)) },
    close() { closed = true; clearTimeout(timer); if (ws) ws.close() },
  }
}
