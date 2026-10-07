// Satu panggilan WebRTC dengan pasangan. Sinyal dilewatkan lewat WebSocket (Lobby di server).
// Penelepon (caller) membuat offer; penerima (callee) menjawab.
export function createCall({ role, iceServers, localStream, send, onRemote, onFail }) {
  const pc = new RTCPeerConnection({ iceServers })
  const pending = []
  let remoteSet = false
  let done = false

  if (localStream) {
    localStream.getTracks().forEach((t) => pc.addTrack(t, localStream))
  } else {
    // Tanpa kamera pun tetap bisa melihat dan mendengar pasangan (hanya menerima).
    // Wajib ada untuk CALLER dan CALLEE: tanpa ini, sisi tanpa kamera tidak membuat
    // m-line di answer dan jalur media tidak terbentuk, sehingga videonya tidak pernah datang.
    pc.addTransceiver('video', { direction: 'recvonly' })
    pc.addTransceiver('audio', { direction: 'recvonly' })
  }

  pc.onicecandidate = (e) => { if (e.candidate) send({ type: 'candidate', candidate: e.candidate }) }
  pc.ontrack = (e) => { if (e.streams[0]) onRemote(e.streams[0]) }
  pc.onconnectionstatechange = () => {
    if (!done && pc.connectionState === 'failed') onFail()
  }

  async function flush() {
    remoteSet = true
    while (pending.length) await pc.addIceCandidate(pending.shift()).catch(() => {})
  }

  async function offer() {
    const o = await pc.createOffer()
    await pc.setLocalDescription(o)
    send({ type: 'offer', sdp: pc.localDescription })
  }

  async function handle(d) {
    if (done || !d) return
    try {
      if (d.type === 'offer') {
        await pc.setRemoteDescription(d.sdp)
        await flush()
        const a = await pc.createAnswer()
        await pc.setLocalDescription(a)
        send({ type: 'answer', sdp: pc.localDescription })
      } else if (d.type === 'answer') {
        await pc.setRemoteDescription(d.sdp)
        await flush()
      } else if (d.type === 'candidate') {
        if (remoteSet) await pc.addIceCandidate(d.candidate).catch(() => {})
        else pending.push(d.candidate)
      }
    } catch {
      onFail()
    }
  }

  if (role === 'caller') offer().catch(onFail)

  return {
    handle,
    close() { done = true; pc.ontrack = null; pc.onicecandidate = null; pc.close() },
  }
}
