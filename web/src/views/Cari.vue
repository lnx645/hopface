<script setup>
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { store, api } from '../store'

// ---- data tab ----
const tab = ref('nearby')
const people = ref([])
const friends = ref([])
const feed = ref([])
const loading = ref(false)
const loadErr = ref('')

// ---- profil orang lain ----
const profile = ref(null)
const profileBusy = ref(false)

// ---- status baru ----
const postText = ref('')
const postFile = ref(null)
const postInput = ref(null)
const posting = ref(false)
const postErr = ref('')

// ---- chat pertemanan ----
const chat = ref(null)          // { id, name, avatar }
const msgs = ref([])
const dmInput = ref('')
const stickerOpen = ref(false)
const dmFile = ref(null)
const dmInputEl = ref(null)
const dmBox = ref(null)
const sending = ref(false)
const dmErr = ref('')
let pollTimer = null

// Stiker klasik ala emoticon forum 2013 (dihasilkan dari cmd/genstickers).
const STICKERS = ['happy', 'sad', 'laugh', 'wink', 'tongue', 'cool', 'surprise', 'cry', 'angry', 'kiss', 'sleepy', 'love']

const me = computed(() => store.me)
// Prefin tampil di listing Cari (default: tampil).
const showMe = computed(() => !(me.value.user && me.value.user.hideNearby))

const MESSAGES = {
  not_friends: 'You are not friends yet. Add them first.',
  hidden: 'This member does not appear in Cari.',
  not_found: 'Member not found.',
  too_large: 'That is too large. Maximum 3 MB.',
  invalid_image: 'That file is not a valid image.',
}

async function loadNearby() {
  loading.value = true
  loadErr.value = ''
  const r = await api('GET', '/api/nearby')
  loading.value = false
  if (r.ok) people.value = r.data.people || []
  else loadErr.value = MESSAGES[r.data && r.data.error] || 'Could not load members.'
}

async function loadFeed() {
  const r = await api('GET', '/api/posts')
  if (r.ok) feed.value = r.data.posts || []
}

async function loadFriends() {
  const r = await api('GET', '/api/friends')
  if (r.ok) friends.value = r.data.friends || []
}

function setTab(t) {
  tab.value = t
  profile.value = null
  if (t === 'nearby') loadNearby()
  if (t === 'status') loadFeed()
  if (t === 'friends') loadFriends()
}

// ---- profil orang lain ----
async function openProfile(id) {
  const r = await api('GET', '/api/nearby/' + encodeURIComponent(id))
  if (r.ok) profile.value = r.data.profile
  else alert(MESSAGES[r.data && r.data.error] || 'Could not open profile.')
}

async function addFriend(p) {
  profileBusy.value = true
  const r = await api('POST', '/api/nearby/' + encodeURIComponent(p.id) + '/friend', {})
  profileBusy.value = false
  if (r.ok) {
    p.isFriend = true
    if (profile.value && profile.value.id === p.id) profile.value.isFriend = true
    loadFriends()
  } else {
    alert(MESSAGES[r.data && r.data.error] || 'Could not add friend.')
  }
}

// ---- pref tampil di Cari ----
async function toggleShow(ev) {
  const show = ev.target.checked
  const r = await api('POST', '/api/nearby/pref', { show })
  if (r.ok && store.me && store.me.user) store.me.user.hideNearby = !show
  else ev.target.checked = !show
}

// ---- posting status ----
function pickPostFile(ev) { postFile.value = ev.target.files[0] || null }

async function createPost() {
  if (!postText.value.trim() && !postFile.value) return
  posting.value = true
  postErr.value = ''
  const fd = new FormData()
  fd.append('text', postText.value.trim())
  if (postFile.value) fd.append('image', postFile.value)
  const r = await fetch('/api/posts', { method: 'POST', credentials: 'same-origin', body: fd })
  posting.value = false
  let data = null
  try { data = await r.json() } catch { /* tubuh kosong */ }
  if (r.ok) {
    postText.value = ''
    postFile.value = null
    if (postInput.value) postInput.value.value = ''
    loadFeed()
  } else {
    postErr.value = (data && MESSAGES[data.error]) || 'Could not post. Please try again.'
  }
}

// ---- chat pertemanan ----
function avatarOf(p) { return p.avatar || '/avatar/default.svg' }

function fmtDist(km) {
  if (km === undefined || km === null || km < 0) return 'Location unknown'
  if (km < 1) return 'less than 1 km away'
  return Math.round(km) + ' km away'
}

function fmtTime(t) {
  const d = new Date(t)
  return d.toLocaleDateString(undefined, { day: '2-digit', month: 'short', year: 'numeric' }) +
    ' ' + d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
}

function lastMs() {
  if (!msgs.value.length) return 0
  return new Date(msgs.value[msgs.value.length - 1].at).getTime()
}

async function pollDM() {
  if (!chat.value) return
  const r = await api('GET', '/api/dm/' + encodeURIComponent(chat.value.id) + '?since=' + lastMs())
  if (!r.ok) return
  const incoming = r.data.messages || []
  if (incoming.length) {
    msgs.value = msgs.value.concat(incoming)
    scrollChat()
  }
}

function startPoll() {
  stopPoll()
  pollTimer = setInterval(pollDM, 4000) // gaya AJAX polling jadul
}
function stopPoll() {
  if (pollTimer) { clearInterval(pollTimer); pollTimer = null }
}

async function openChat(p) {
  chat.value = { id: p.id, name: p.name, avatar: avatarOf(p) }
  profile.value = null
  msgs.value = []
  dmErr.value = ''
  stickerOpen.value = false
  const r = await api('GET', '/api/dm/' + encodeURIComponent(p.id))
  if (r.ok) msgs.value = r.data.messages || []
  startPoll()
  scrollChat()
}

function closeChat() {
  stopPoll()
  chat.value = null
  msgs.value = []
}

function scrollChat() {
  nextTick(() => {
    if (dmBox.value) dmBox.value.scrollTop = dmBox.value.scrollHeight
  })
}

async function sendDM(kind, text) {
  if (sending.value) return
  sending.value = true
  dmErr.value = ''
  const fd = new FormData()
  fd.append('kind', kind)
  fd.append('text', text || '')
  const r = await fetch('/api/dm/' + encodeURIComponent(chat.value.id), { method: 'POST', credentials: 'same-origin', body: fd })
  sending.value = false
  let data = null
  try { data = await r.json() } catch { /* tubuh kosong */ }
  if (r.ok) {
    msgs.value.push(data.message)
    scrollChat()
  } else {
    dmErr.value = (data && MESSAGES[data.error]) || 'Could not send. Please try again.'
  }
}

function sendText() {
  const t = dmInput.value.trim()
  if (!t) return
  dmInput.value = ''
  sendDM('text', t)
}

function sendSticker(name) {
  stickerOpen.value = false
  sendDM('sticker', name)
}

function pickDMFile(ev) {
  const f = ev.target.files[0]
  ev.target.value = ''
  if (!f || !chat.value) return
  const fd = new FormData()
  fd.append('kind', 'image')
  fd.append('text', '')
  fd.append('image', f)
  fetch('/api/dm/' + encodeURIComponent(chat.value.id), { method: 'POST', credentials: 'same-origin', body: fd })
    .then((r) => r.json().then((d) => { if (r.ok) { msgs.value.push(d.message); scrollChat() } else { dmErr.value = (d && MESSAGES[d.error]) || 'Could not send image.' } }))
    .catch(() => { dmErr.value = 'Could not send. Please try again.' })
}

onMounted(() => { loadNearby() })
onUnmounted(stopPoll)
</script>

<template>
  <div class="cari">
    <!-- Tab gaya "web 2013": folder tabs ala browser jadul. -->
    <div class="tabs" role="tablist">
      <button type="button" :class="['tab', { on: tab === 'nearby' && !profile && !chat }]" @click="setTab('nearby')">
        <i class="icon-search"></i> Nearby
      </button>
      <button type="button" :class="['tab', { on: tab === 'status' && !profile && !chat }]" @click="setTab('status')">
        <i class="icon-comment-alt"></i> Status
      </button>
      <button type="button" :class="['tab', { on: tab === 'friends' && !profile && !chat }]" @click="setTab('friends')">
        <i class="icon-user"></i> Friends
      </button>
    </div>

    <!-- ===== Chat pertemanan ===== -->
    <div v-if="chat" class="card dmpage">
      <div class="dmhead">
        <button class="btn small" type="button" @click="closeChat"><i class="icon-arrow-left"></i> Back</button>
        <img class="dmava" :src="chat.avatar" alt="" width="32" height="32">
        <b>{{ chat.name }}</b>
      </div>
      <div ref="dmBox" class="dmbox">
        <div v-if="!msgs.length" class="center muted" style="padding:1rem">No messages yet. Say hi!</div>
        <div v-for="m in msgs" :key="m.id" :class="['dmb', m.from === (me.user && me.user.id) ? 'mine' : 'theirs']">
          <template v-if="m.kind === 'text'">{{ m.text }}</template>
          <img v-else-if="m.kind === 'sticker'" :src="'/img/sticker/' + m.text + '.png'" alt="" class="sticker" width="64" height="64">
          <a v-else-if="m.kind === 'image'" :href="'/img/uploads/' + m.text" target="_blank" rel="noopener">
            <img :src="'/img/uploads/' + m.text" alt="image" class="dmimg">
          </a>
          <div class="dmtime">{{ fmtTime(m.at) }}</div>
        </div>
      </div>
      <div v-if="dmErr" class="err">{{ dmErr }}</div>
      <div v-if="stickerOpen" class="stickerbox">
        <button v-for="s in STICKERS" :key="s" type="button" class="stickerbtn" :title="s" @click="sendSticker(s)">
          <img :src="'/img/sticker/' + s + '.png'" :alt="s" width="40" height="40">
        </button>
      </div>
      <div class="dmbar">
        <button class="btn small" type="button" title="Stickers" @click="stickerOpen = !stickerOpen"><i class="icon-meh"></i></button>
        <button class="btn small" type="button" title="Send image" @click="$refs.dmFile.click()"><i class="icon-picture"></i></button>
        <input ref="dmFile" type="file" accept="image/*" class="hidden" @change="pickDMFile">
        <input ref="dmInputEl" v-model="dmInput" type="text" class="dminput" placeholder="Type a message…"
               maxlength="500" @keyup.enter="sendText">
        <button class="btn green small" type="button" :disabled="sending || !dmInput.trim()" @click="sendText">Send</button>
      </div>
    </div>

    <!-- ===== Profil orang lain ===== -->
    <div v-else-if="profile" class="card">
      <button class="btn small" type="button" @click="profile = null"><i class="icon-arrow-left"></i> Back</button>
      <div class="phead">
        <img :src="profile.avatar" alt="" width="72" height="72" class="pava">
        <div>
          <div class="pname">{{ profile.name }}, {{ profile.age }}</div>
          <div class="muted">{{ profile.gender === 'male' ? 'Man' : profile.gender === 'female' ? 'Woman' : 'Other' }}
            <template v-if="profile.city"> · {{ profile.city }}</template></div>
          <div class="muted"><i class="icon-map-marker"></i> {{ fmtDist(profile.distanceKm) }}</div>
        </div>
      </div>
      <div class="pactions">
        <button v-if="!profile.isFriend" class="btn green" type="button" :disabled="profileBusy" @click="addFriend(profile)">
          <i class="icon-plus"></i> Add friend
        </button>
        <span v-else class="ok"><i class="icon-ok"></i> You are friends</span>
        <button v-if="profile.isFriend" class="btn" type="button" @click="openChat(profile)">
          <i class="icon-comment"></i> Chat
        </button>
      </div>
      <h3 class="phd">Latest status</h3>
      <div v-if="!profile.posts || !profile.posts.length" class="muted center" style="padding:.6rem">No status yet.</div>
      <div v-for="p in profile.posts" :key="p.id" class="post">
        <div class="muted">{{ fmtTime(p.at) }}</div>
        <p v-if="p.text" class="ptext">{{ p.text }}</p>
        <img v-if="p.image" :src="'/img/uploads/' + p.image" alt="" class="pimg">
      </div>
    </div>

    <!-- ===== Chat page guard: content dibawah ===== -->
    <template v-else>
      <!-- Nearby -->
      <div v-if="tab === 'nearby'" class="card">
        <h3 class="phd"><i class="icon-search"></i> Members near you</h3>
        <label class="check" style="margin:.2rem 0 .8rem">
          <input type="checkbox" :checked="showMe" @change="toggleShow">
          Show me in Cari (location is only a rough city from your IP, never exact)
        </label>
        <div v-if="loading" class="center"><img src="/img/ajax-loader.gif" alt="" width="28" height="28"> Loading…</div>
        <div v-else-if="loadErr" class="err">{{ loadErr }}</div>
        <div v-else-if="!people.length" class="muted center" style="padding:.6rem">
          No other members here yet. Members appear once their city is detected at login.
        </div>
        <div v-for="p in people" :key="p.id" class="prow">
          <img :src="avatarOf(p)" alt="" width="44" height="44" class="prava">
          <a href="#" class="plink" @click.prevent="openProfile(p.id)">
            <b>{{ p.name }}</b><span class="muted">, {{ p.age }}</span>
            <div class="muted"><i class="icon-map-marker"></i> {{ p.city || 'Unknown city' }} · {{ fmtDist(p.distanceKm) }}</div>
          </a>
          <span class="pacts">
            <button v-if="!p.isFriend" class="btn small green" type="button" @click="addFriend(p)">Add</button>
            <button v-else class="btn small" type="button" @click="openChat(p)">Chat</button>
          </span>
        </div>
      </div>

      <!-- Status -->
      <div v-if="tab === 'status'">
        <div class="card">
          <h3 class="phd"><i class="icon-pencil"></i> Post a status</h3>
          <textarea v-model="postText" rows="3" maxlength="500" placeholder="What's on your mind? (2013 style)"></textarea>
          <div class="prow2">
            <label class="btn small" style="margin:0">
              <i class="icon-picture"></i> {{ postFile ? postFile.name : 'Attach photo' }}
              <input ref="postInput" type="file" accept="image/*" class="hidden" @change="pickPostFile">
            </label>
            <button class="btn red" type="button" :disabled="posting || (!postText.trim() && !postFile)" @click="createPost">
              <img v-if="posting" src="/img/ajax-loader.gif" alt="" width="16" height="16" style="vertical-align:-3px">
              {{ posting ? 'Posting…' : 'Post' }}
            </button>
          </div>
          <div v-if="postErr" class="err">{{ postErr }}</div>
        </div>
        <div v-if="!feed.length" class="card muted center">No status yet. Be the first!</div>
        <div v-for="p in feed" :key="p.id" class="card postcard">
          <div class="prow">
            <img :src="p.avatar" alt="" width="44" height="44" class="prava">
            <a href="#" class="plink" @click.prevent="openProfile(p.userId)">
              <b>{{ p.name }}</b>
              <div class="muted">{{ fmtTime(p.at) }}</div>
            </a>
          </div>
          <p v-if="p.text" class="ptext">{{ p.text }}</p>
          <img v-if="p.image" :src="'/img/uploads/' + p.image" alt="" class="pimg">
        </div>
      </div>

      <!-- Friends -->
      <div v-if="tab === 'friends'" class="card">
        <h3 class="phd"><i class="icon-user"></i> My friends</h3>
        <div v-if="!friends.length" class="muted center" style="padding:.6rem">
          No friends yet. Find people in <b>Nearby</b> and add them.
        </div>
        <div v-for="p in friends" :key="p.id" class="prow">
          <img :src="avatarOf(p)" alt="" width="44" height="44" class="prava">
          <a href="#" class="plink" @click.prevent="openProfile(p.id)">
            <b>{{ p.name }}</b><span class="muted">, {{ p.age }}</span>
            <div class="muted"><i class="icon-map-marker"></i> {{ p.city || 'Unknown city' }} · {{ fmtDist(p.distanceKm) }}</div>
          </a>
          <span class="pacts">
            <button class="btn small green" type="button" @click="openChat(p)"><i class="icon-comment"></i> Chat</button>
          </span>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.cari { padding-top: .8rem }

/* Folder tabs ala browser/OS jadul. */
.tabs { display: flex; gap: 2px; border-bottom: 1px solid #a5a5a5; margin-bottom: .7rem; overflow-x: auto }
.tab {
  font-family: Verdana, Tahoma, 'DejaVu Sans', Geneva, sans-serif; font-size: .8rem; font-weight: 700;
  background: linear-gradient(#f4f4f4, #dcdcdc); border: 1px solid #a5a5a5; border-bottom: 0;
  border-radius: 5px 5px 0 0; padding: .4rem .9rem; min-height: 34px; color: #555; white-space: nowrap;
  box-shadow: inset 0 1px 0 #fff;
}
.tab.on { background: linear-gradient(#ffffff, #eeeeee); color: #b30000; position: relative; top: 1px; border-bottom: 1px solid #eeeeee }
.tab i { text-shadow: 0 1px 0 #fff }

.phd { font-family: var(--serif); font-size: 1.05rem; margin: 0 0 .6rem; border-bottom: 1px solid var(--line-2); padding-bottom: .35rem }
.phd i { color: var(--red); text-shadow: 0 1px 0 #fff }

/* Baris daftar orang */
.prow { display: flex; align-items: center; gap: .6rem; padding: .5rem .2rem; border-bottom: 1px dashed var(--line-2) }
.prow:last-child { border-bottom: 0 }
.prava { border: 1px solid #b8b8b8; border-radius: 4px; background: #fff; box-shadow: 0 1px 1px rgba(0,0,0,.1); flex: none; object-fit: cover }
.plink { flex: 1 1 auto; min-width: 0; color: var(--tx) }
.plink:hover { text-decoration: none }
.plink b { color: #0033cc }
.plink b:hover { text-decoration: underline }
.pacts { flex: none; display: flex; gap: .35rem }
.prow2 { display: flex; justify-content: space-between; align-items: center; gap: .5rem; margin-top: .6rem; flex-wrap: wrap }

/* Profil */
.phead { display: flex; gap: .8rem; align-items: center; margin: .8rem 0 }
.pava { border: 1px solid #b8b8b8; border-radius: 5px; background: #fff; box-shadow: 0 1px 3px rgba(0,0,0,.15); object-fit: cover }
.pname { font-family: var(--serif); font-size: 1.25rem; font-weight: 700 }
.pactions { display: flex; gap: .5rem; align-items: center; flex-wrap: wrap; margin-bottom: .6rem }

/* Status */
.ptext { white-space: pre-wrap; overflow-wrap: anywhere; margin: .35rem 0 .5rem }
.pimg { max-width: 100%; border: 1px solid #c4c4c4; border-radius: 4px; background: #fff; padding: 3px; box-shadow: 0 1px 2px rgba(0,0,0,.12); display: block }
.post { border-top: 1px dashed var(--line-2); padding: .5rem 0 }
.postcard .prow { border-bottom: 0; padding-bottom: .2rem }

/* Chat pertemanan */
.dmhead { display: flex; align-items: center; gap: .55rem; margin-bottom: .5rem }
.dmava { border: 1px solid #b8b8b8; border-radius: 4px; background: #fff }
.dmbox {
  display: flex; flex-direction: column; gap: .35rem; min-height: 220px; max-height: 55vh; overflow-y: auto;
  background: linear-gradient(#f7f7f7, #ececec); border: 1px solid #c4c4c4; border-radius: 4px;
  padding: .6rem; scrollbar-width: thin; scrollbar-color: #b0b0b0 #e6e6e6;
}
.dmb {
  max-width: 78%; padding: .4rem .6rem; border: 1px solid; border-radius: 6px; font-size: .9rem;
  align-self: flex-start; overflow-wrap: anywhere; box-shadow: 0 1px 1px rgba(0,0,0,.08);
}
.dmb.mine {
  align-self: flex-end; background: linear-gradient(#fff0f0, #ffe0e0); border-color: #e0a0a0; text-align: right;
}
.dmb.theirs { background: linear-gradient(#ffffff, #eef4ff); border-color: #9fb6dd }
.dmtime { font-size: .64rem; color: var(--dim); margin-top: .18rem; font-family: Verdana, Tahoma, sans-serif }
.sticker { display: block; image-rendering: auto }
.dmimg { max-width: 200px; max-height: 200px; display: block; border-radius: 3px }
.dmbar { display: flex; gap: .4rem; margin-top: .5rem; align-items: center }
.dminput { flex: 1 1 auto; min-width: 0 }
.stickerbox {
  display: grid; grid-template-columns: repeat(6, 1fr); gap: .3rem; margin-top: .5rem;
  background: linear-gradient(#fafafa, #efefef); border: 1px solid #c4c4c4; border-radius: 4px; padding: .5rem;
}
.stickerbtn { background: none; border: 1px solid transparent; border-radius: 4px; padding: .15rem; min-height: 44px }
.stickerbtn:hover { background: #fff; border-color: #b8b8b8 }

@media (max-width: 560px) {
  .stickerbox { grid-template-columns: repeat(4, 1fr) }
  .dmb { max-width: 88% }
}
</style>
