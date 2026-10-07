<script setup>
import { ref, reactive, computed, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import CustomSelect from './CustomSelect.vue'
import { connectSocket } from '../socket'
import { store } from '../store'
import { createCall } from '../rtc'
import { api } from '../store'
import { countries, flag, countryName } from '../countries'

const genderFilterOptions = [
  { value: '', label: 'Anyone' },
  { value: 'female', label: 'Female' },
  { value: 'male', label: 'Male' },
  { value: 'other', label: 'Other' },
]
const countryFilterOptions = [{ value: '', label: 'Anywhere' }, ...countries.map((c) => ({ value: c.code, label: c.name }))]
const reportOptions = computed(() => REASONS.map((r) => ({ value: r, label: r })))
const pct = (v) => Math.round(((Math.min(99, Math.max(18, +v || 18)) - 18) / 81) * 100) + '%'

const RELAX_MS = 10000 // harus sama dengan batas di server (Matchmaker.RelaxAfter)

const link = ref('connecting') // connecting | open | closed
const state = ref('idle') // idle | searching | chatting (mengikuti server)
const peer = ref(null)
const msgs = ref([])
const draft = ref('')
const online = ref(0)
const waiting = ref(0)
const notice = ref('')
const camNote = ref('')
const textOnly = ref(false)
const remoteReady = ref(false)
const videoFailed = ref(false)
const showReport = ref(false)
const reportReason = ref('Nudity or sexual content')
const reportSent = ref(false)
const searchSince = ref(0)
const now = ref(Date.now())

const filter = reactive({ gender: '', minAge: 18, maxAge: 99, country: '' })

const remoteEl = ref(null)
const localEl = ref(null)
const msgsEl = ref(null)

// Di layar lebar panel filter terbuka; di HP dilipat agar video dan chat langsung terlihat.
const wide = window.matchMedia('(min-width: 721px)')
const filtersOpen = ref(wide.matches)

let sock = null
let localStream = null
let callPromise = null
let callToken = 0
let iceCache = null
let timer = null
let snapTimer = null
let lastFrame = '' // frame video pasangan terakhir, jaringan pengaman bila video sempat tak siap saat lapor

const REASONS = ['Nudity or sexual content', 'Harassment or abuse', 'Appears to be under 18', 'Spam or advertising', 'Other']
const ERRORS = {
  banned: 'This account has been suspended.',
  replaced: 'Hopface is open in another tab or device. Close it and reload this page to continue here.',
  slow_down: 'You are sending messages too fast.',
  message_too_long: 'That message is too long.',
  rate_limited: 'Too many requests. Please slow down.',
  report_failed: 'Could not send the report. Try again.',
}
const GENDER_LABEL = { '': 'Anyone', female: 'Female', male: 'Male', other: 'Other' }

const busy = computed(() => state.value !== 'idle')
// Di HP, saat mengetik pesan kunci tombol Next/Stop supaya tidak salah kepencet pengganti Send.
const mobileTypingLock = computed(() => !wide.matches && state.value === 'chatting' && draft.value.trim() !== '')
const meAvatar = computed(() => (store.me && store.me.avatar) || '/avatar/default.svg')
const relaxed = computed(() => state.value === 'searching' && now.value - searchSince.value >= RELAX_MS)
const secs = computed(() => Math.max(0, Math.floor((now.value - searchSince.value) / 1000)))
// Ringkasan filter yang tampil di judul panel saat dilipat.
const filterSummary = computed(() => {
  const f = currentFilter()
  const where = f.country ? countryName(f.country) : 'Anywhere'
  return `${GENDER_LABEL[f.gender] || 'Anyone'} · ${f.minAge}–${f.maxAge} · ${where}`
})

function sys(text) { msgs.value.push({ k: 'sys', text }) }

watch(() => msgs.value.length, async () => {
  await nextTick()
  if (msgsEl.value) msgsEl.value.scrollTop = msgsEl.value.scrollHeight
})

// Saat mulai mencari di HP, lipat filter supaya video dan chat memenuhi layar.
watch(state, (s) => {
  if (!wide.matches) filtersOpen.value = s === 'idle' ? filtersOpen.value : false
})

// ---- media & panggilan ----

async function ensureMedia() {
  camNote.value = ''
  if (textOnly.value || localStream) return
  try {
    localStream = await navigator.mediaDevices.getUserMedia({ video: { width: 640, height: 480 }, audio: true })
    await nextTick()
    if (localEl.value) localEl.value.srcObject = localStream
  } catch {
    localStream = null
    camNote.value = 'Camera or microphone is unavailable, so you can only watch and chat. Allow access in your browser to share video.'
  }
}

function stopMedia() {
  if (localStream) localStream.getTracks().forEach((t) => t.stop())
  localStream = null
  if (localEl.value) localEl.value.srcObject = null
}

async function iceServers() {
  if (iceCache && Date.now() - iceCache.at < 20 * 60 * 1000) return iceCache.list
  const r = await api('GET', '/api/ice')
  const list = r.ok && r.data && r.data.iceServers ? r.data.iceServers : [{ urls: ['stun:stun.cloudflare.com:3478'] }]
  iceCache = { list, at: Date.now() }
  return list
}

function startCall(role) {
  stopCall()
  const token = ++callToken
  remoteReady.value = false
  callPromise = (async () => {
    const servers = await iceServers()
    if (token !== callToken) return null
    return createCall({
      role,
      iceServers: servers,
      localStream,
      send: (data) => sock && sock.send({ t: 'signal', data }),
      onRemote: (stream) => {
        if (token !== callToken) return
        if (remoteEl.value) remoteEl.value.srcObject = stream
        remoteReady.value = true
      },
      onFail: () => {
        if (token === callToken) {
          videoFailed.value = true
          sys('The video connection failed. You can still use the text chat, or press Next.')
        }
      },
    })
  })()
}

function stopCall() {
  callToken++
  const p = callPromise
  callPromise = null
  if (p) p.then((c) => c && c.close()).catch(() => {})
  if (remoteEl.value) remoteEl.value.srcObject = null
  remoteReady.value = false
}

// ---- pesan dari server ----

function onEvent(e) {
  switch (e.t) {
    case 'state':
      state.value = e.state
      online.value = e.online || 0
      waiting.value = e.waiting || 0
      if (e.state === 'searching') {
        searchSince.value = Date.now()
        peer.value = null
        stopCall()
        msgs.value = [] // mulai pencarian baru: riwayat chat lama dihapus
        lastFrame = ''  // frame pasangan sebelumnya dibuang, tidak boleh terbawa ke match berikutnya
      } else if (e.state === 'idle') {
        peer.value = null
        stopCall()
        msgs.value = [] // Stop ditekan: riwayat chat ikut hilang
        lastFrame = ''  // selesai sesi: frame sementara langsung dihapus
      }
      break
    case 'matched':
      state.value = 'chatting'
      peer.value = e.peer
      msgs.value = []
      lastFrame = '' // bukti frame milik pasangan sebelumnya tidak boleh tercampur
      reportSent.value = false
      showReport.value = false
      videoFailed.value = false
      sys(`You are connected with ${e.peer.name} (${e.peer.age}, ${countryName(e.peer.country)}). Say hi!`)
      startCall(e.role)
      break
    case 'partner_left':
      stopCall()
      peer.value = null
      lastFrame = '' // orang sudah selesai: frame sementara langsung dibuang
      showReport.value = false
      videoFailed.value = false
      sys('Your partner has left the chat.')
      break
    case 'chat':
      msgs.value.push({ k: 'peer', text: e.text })
      break
    case 'signal':
      if (callPromise) callPromise.then((c) => c && c.handle(e.data))
      break
    case 'reported':
      reportSent.value = true
      sys('Report sent. Thank you. You were moved to someone new.')
      break
    case 'error':
      if (e.error === 'banned' || e.error === 'replaced') {
        notice.value = ERRORS[e.error]
        stopCall()
        stopMedia()
        videoFailed.value = false
        state.value = 'idle'
      } else {
        sys(ERRORS[e.error] || 'Something went wrong.')
      }
      break
  }
}

// ---- aksi pengguna ----

function currentFilter() {
  let lo = Math.min(Math.max(+filter.minAge || 18, 18), 99)
  let hi = Math.min(Math.max(+filter.maxAge || 99, 18), 99)
  if (lo > hi) [lo, hi] = [hi, lo]
  return { gender: filter.gender, minAge: lo, maxAge: hi, country: filter.country }
}

async function start() {
  notice.value = ''
  await ensureMedia()
  const f = currentFilter()
  localStorage.setItem('hopface.filter', JSON.stringify({ ...f, textOnly: textOnly.value }))
  sock.send({ t: 'start', filter: f })
}

function next() { sock.send({ t: 'next' }) }

function stop() {
  sock.send({ t: 'stop' })
  stopCall()
  stopMedia()
}

function say() {
  const text = draft.value.trim()
  if (!text || state.value !== 'chatting') return
  sock.send({ t: 'chat', text })
  msgs.value.push({ k: 'me', text })
  draft.value = ''
}

// Ambil satu frame video pasangan sebagai bukti laporan (data URL JPEG kecil).
function grabFrame() {
  try {
    const v = remoteEl.value
    if (!v || !v.videoWidth || !v.videoHeight) return ''
    const c = document.createElement('canvas')
    const render = (w, quality) => {
      c.width = w
      c.height = Math.round((v.videoHeight * w) / v.videoWidth)
      c.getContext('2d').drawImage(v, 0, 0, c.width, c.height)
      return c.toDataURL('image/jpeg', quality)
    }
    let url = render(320, 0.55)
    if (url.length > 400_000) url = render(240, 0.4) // sesuai batas server (Lobby.Report)
    return url.length > 400_000 ? '' : url
  } catch {
    return '' // video belum siap / diblokir browser: laporan tetap terkirim tanpa bukti
  }
}

function sendReport() {
  sock.send({ t: 'report', reason: reportReason.value, frame: grabFrame() || lastFrame })
  showReport.value = false
}

onMounted(() => {
  try {
    const s = JSON.parse(localStorage.getItem('hopface.filter') || 'null')
    if (s) { Object.assign(filter, { gender: s.gender || '', minAge: s.minAge || 18, maxAge: s.maxAge || 99, country: s.country || '' }); textOnly.value = !!s.textOnly }
  } catch { /* abaikan filter tersimpan yang rusak */ }
  sock = connectSocket({ onEvent, onStatus: (s) => { link.value = s; if (s === 'closed') { state.value = 'idle'; peer.value = null; stopCall() } } })
  timer = setInterval(() => { now.value = Date.now() }, 1000)
  // Ambil frame pasangan berkala selama chat supaya bukti selalu ada saat laporan dikirim.
  snapTimer = setInterval(() => {
    if (state.value === 'chatting') { const f = grabFrame(); if (f) lastFrame = f }
  }, 5000)
})

onBeforeUnmount(() => {
  clearInterval(timer)
  clearInterval(snapTimer)
  stopCall()
  stopMedia()
  if (sock) sock.close()
})
</script>

<template>
  <div class="chat">
    <div v-if="notice" class="banner">{{ notice }}</div>
    <div v-if="camNote" class="banner info">{{ camNote }}</div>

    <details class="card fbox" :open="filtersOpen" @toggle="filtersOpen = $event.target.open">
      <summary>
        Who do you want to meet?
        <span class="hint">tap to change</span>
        <span class="sum">{{ filterSummary }}</span>
      </summary>
      <div class="filters">
        <div class="field">
          <label>Gender</label>
          <CustomSelect v-model="filter.gender" :options="genderFilterOptions" :disabled="busy" />
        </div>
        <div class="field">
          <label>Country</label>
          <CustomSelect v-model="filter.country" :options="countryFilterOptions" :disabled="busy" />
        </div>
        <div class="field wide">
          <label>Age: <b>{{ filter.minAge }} – {{ filter.maxAge }}</b></label>
          <input type="range" :style="{ '--p': pct(filter.minAge) }" v-model.number="filter.minAge" min="18" max="99" :disabled="busy" aria-label="Age from" @input="filter.minAge > filter.maxAge && (filter.maxAge = filter.minAge)">
          <input type="range" :style="{ '--p': pct(filter.maxAge) }" v-model.number="filter.maxAge" min="18" max="99" :disabled="busy" aria-label="Age to" @input="filter.maxAge < filter.minAge && (filter.minAge = filter.maxAge)">
        </div>
        <div class="field wide">
          <label class="check"><input type="checkbox" v-model="textOnly" :disabled="busy"> Text chat only (no camera or microphone)</label>
        </div>
      </div>
    </details>

    <div class="stage">
      <div class="videos">
        <video ref="remoteEl" class="remote" autoplay playsinline></video>
        <video ref="localEl" class="local" autoplay playsinline muted v-show="!textOnly"></video>

        <div v-if="state === 'idle'" class="overlay">
          <img v-if="link !== 'open'" class="spin" src="/img/ajax-loader.gif" alt="" width="28" height="28">
          <b>{{ link === 'open' ? 'Press Start to find someone' : 'Connecting…' }}</b>
        </div>
        <div v-else-if="state === 'searching'" class="overlay">
          <img class="spin" src="/img/ajax-loader.gif" alt="" width="28" height="28">
          <b>Looking for someone…</b>
          <span v-if="!relaxed">Matching your filter ({{ secs }}s)</span>
          <span v-else>No exact match yet, so we are now matching you with anyone.</span>
          <span>{{ waiting }} waiting</span>
        </div>
        <div v-else-if="state === 'chatting' && videoFailed" class="overlay">
          <img class="pav-lg" v-if="peer" :src="peer.picture" alt="Partner avatar" width="72" height="72">
          <b v-if="peer">{{ peer.name }}, {{ peer.age }} · {{ countryName(peer.country) }}</b>
          <b style="margin-top:.5rem">Video connection failed</b>
          <span>You can still chat in text, or press <b>Next</b>.</span>
        </div>
        <div v-else-if="state === 'chatting' && !remoteReady" class="overlay">
          <img class="pav-lg" v-if="peer" :src="peer.picture" alt="Partner avatar" width="72" height="72">
          <b v-if="peer">{{ peer.name }}, {{ peer.age }}</b>
          <span v-if="peer">{{ flag(peer.country) }} {{ countryName(peer.country) }}</span>
          <img class="spin" src="/img/ajax-loader.gif" alt="" width="28" height="28">
          <span>Connecting video… you can still chat in text. If the other side also has “text chat only”, no video will stream.</span>
        </div>
      </div>

      <div class="chatbox">
        <div class="peerbar">
          <img class="pav" :src="meAvatar" alt="You" width="20" height="20"> <b class="you">You</b>
          <span class="arrow">⇄</span>
          <template v-if="peer">
            <img class="pav" :src="peer.picture" alt="" width="20" height="20"> <b>{{ peer.name }}, {{ peer.age }}</b>
            <span class="country">{{ flag(peer.country) }} {{ countryName(peer.country) }}</span>
          </template>
          <span v-else class="country">No partner yet</span>
        </div>
        <div ref="msgsEl" class="msgs" aria-live="polite">
          <div v-if="!msgs.length" class="msg sys">Messages will appear here.</div>
          <div v-for="(m, i) in msgs" :key="i" class="msg" :class="m.k">
            <template v-if="m.k === 'me'"><b>You:</b> {{ m.text }}</template>
            <template v-else-if="m.k === 'peer'"><b>{{ peer ? peer.name : 'Stranger' }}:</b> {{ m.text }}</template>
            <template v-else>{{ m.text }}</template>
          </div>
        </div>
        <form class="say" @submit.prevent="say">
          <input type="text" v-model="draft" maxlength="500" placeholder="Type a message…" :disabled="state !== 'chatting'" autocomplete="off" enterkeyhint="send">
          <button type="submit" :disabled="state !== 'chatting' || !draft.trim()">Send</button>
        </form>
      </div>
    </div>

    <!-- Bilah aksi: menempel di dasar layar pada HP, di bawah filter pada layar lebar -->
    <div class="dock">
      <div v-if="showReport" class="banner info">
        <label>Why are you reporting this person?</label>
        <CustomSelect v-model="reportReason" :options="reportOptions" style="margin-bottom:.5rem" />
        <div class="row">
          <button class="btn red small" type="button" @click="sendReport">Send report and skip</button>
          <button class="btn small" type="button" @click="showReport = false">Cancel</button>
        </div>
      </div>
      <div class="row">
        <button v-if="state === 'idle'" class="btn green big" type="button" :disabled="link !== 'open'" @click="start"><i class="icon-play"></i> Start</button>
        <template v-else>
          <button v-if="state === 'chatting'" class="btn green big" type="button" :disabled="mobileTypingLock" @click="next"><i class="icon-forward"></i> Next</button>
          <button class="btn red big" type="button" :disabled="mobileTypingLock" @click="stop"><i class="icon-stop"></i> Stop</button>
          <button v-if="state === 'chatting'" class="btn" type="button" :disabled="reportSent" @click="showReport = !showReport"><i class="icon-flag"></i> Report</button>
        </template>
        <span class="status">
          <template v-if="link !== 'open'"><img src="/img/ajax-loader.gif" alt="" width="14" height="14" style="vertical-align:-3px;margin-right:4px">Connecting to server…</template>
          <span v-else class="online"><b>{{ online }}</b> online now</span>
        </span>
      </div>
    </div>
  </div>
</template>
