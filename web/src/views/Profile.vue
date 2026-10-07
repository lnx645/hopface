<script setup>
import { ref, computed } from 'vue'
import { store, api, loadMe } from '../store'
import { countries } from '../countries'

const emit = defineEmits(['done'])

const me = computed(() => store.me)
const user = computed(() => me.value.user || {})

// Tanggal lahir terbaru yang masih sah (hari ini - 18 tahun) dipakai sebagai batas input.
const latest = (() => {
  const d = new Date()
  d.setFullYear(d.getFullYear() - 18)
  return d.toISOString().slice(0, 10)
})()

const name = ref(user.value.name || '')
const birthdate = ref(user.value.birthdate || '')
const gender = ref(user.value.gender || '')
const country = ref(user.value.country || '')
const adult = ref(!!user.value.birthdate)
const busy = ref(false)
const error = ref('')
const okMsg = ref('')
const file = ref(null)
const fileInput = ref(null)
const avatarBusy = ref(false)

const isNew = computed(() => !user.value.profileComplete)
const canSave = computed(() => name.value.trim() && gender.value && country.value && adult.value && !busy.value)
const preview = computed(() => me.value.avatar || '/avatar/default.svg')

const MESSAGES = { underage: 'You must be at least 18 years old.', invalid_profile: 'Please check your details.', invalid_image: 'That file is not a valid image.' }

async function save() {
  error.value = ''
  okMsg.value = ''
  busy.value = true
  const r = await api('POST', '/api/profile', { name: name.value.trim(), birthdate: birthdate.value, gender: gender.value, country: country.value })
  busy.value = false
  if (r.ok) {
    okMsg.value = 'Saved.'
    await emitDone()
    return
  }
  error.value = MESSAGES[r.data && r.data.error] || 'Something went wrong. Please try again.'
}

async function emitDone() {
  await loadMe()
  emit('done')
}

function onFileChange(e) {
  file.value = e.target.files && e.target.files[0]
  okMsg.value = ''
  error.value = ''
}

async function upload() {
  if (!file.value) return
  avatarBusy.value = true
  const fd = new FormData()
  fd.append('avatar', file.value)
  try {
    const r = await fetch('/api/profile/avatar', { method: 'POST', body: fd, credentials: 'same-origin' })
    avatarBusy.value = false
    const data = await r.json().catch(() => null)
    if (r.ok) { okMsg.value = 'Avatar updated.'; file.value = null; if (fileInput.value) fileInput.value.value = ''; await emitDone() }
    else error.value = MESSAGES[data && data.error] || 'Upload failed. Use a JPEG/PNG image up to 2MB.'
  } catch {
    avatarBusy.value = false
    error.value = 'Network error. Try again.'
  }
}

async function resetAvatar() {
  avatarBusy.value = true
  const r = await api('POST', '/api/profile/avatar/reset')
  avatarBusy.value = false
  if (r.ok) { okMsg.value = 'Avatar reset to Google/default.'; await emitDone() }
}
</script>

<template>
  <div class="card">
    <h1>{{ isNew ? 'Complete your profile' : 'Your profile' }}</h1>
    <p class="lede">{{ isNew ? 'Hello ' + user.name + '! A few details and then you can start.' : 'Update your name, photo, and settings.' }}</p>

    <div class="ava-row">
      <img class="ava-lg" :src="preview" alt="Your avatar" width="72" height="72">
      <div>
        <input ref="fileInput" type="file" accept="image/jpeg,image/png,image/gif,image/webp" @change="onFileChange" style="display:none">
        <button class="btn small" type="button" @click="fileInput.click()">Choose photo</button>
        <button v-if="file" class="btn green small" type="button" :disabled="avatarBusy" @click="upload">Upload</button>
        <button v-if="user.avatarCustom" class="btn small" type="button" :disabled="avatarBusy" @click="resetAvatar">Use Google avatar</button>
        <p class="muted">JPEG, PNG, GIF or WebP, maximum 2MB.</p>
      </div>
    </div>

    <form class="form" @submit.prevent="save">
      <div class="field">
        <label>Email</label>
        <p style="padding:.45rem 0">{{ user.email }}</p>
      </div>
      <div class="field">
        <label for="nm">Display name</label>
        <input id="nm" type="text" v-model="name" maxlength="60" required>
      </div>
      <div class="field">
        <label for="g">I am</label>
        <select id="g" v-model="gender" required>
          <option value="" disabled>Select…</option>
          <option value="male">Male</option>
          <option value="female">Female</option>
          <option value="other">Other</option>
        </select>
      </div>
      <div class="field">
        <label for="c">Country</label>
        <select id="c" v-model="country" required>
          <option value="" disabled>Select…</option>
          <option v-for="c in countries" :key="c.code" :value="c.code">{{ c.name }}</option>
        </select>
      </div>
      <div class="field">
        <label v-if="user.birthdate">Date of birth (locked)</label>
        <label v-else for="bd">Date of birth</label>
        <p v-if="user.birthdate" style="padding:.45rem 0">{{ user.birthdate }} — age {{ me.age }}</p>
        <template v-else>
          <input id="bd" type="date" v-model="birthdate" :max="latest" min="1900-01-01" required>
          <p class="muted">Used to show your age and keep Hopface adults-only. It cannot be changed later.</p>
        </template>
      </div>
      <div class="field" v-if="!user.birthdate">
        <label class="check"><input type="checkbox" v-model="adult"> I confirm I am 18 or older and the details above are true.</label>
      </div>

      <p v-if="error" class="err">{{ error }}</p>
      <p v-if="okMsg" class="ok">{{ okMsg }}</p>
      <button class="btn red big" type="submit" :disabled="!canSave">Save</button>
      <p v-if="!canSave" class="muted" style="margin-top:.5rem">Fill in every field (and tick the confirmation for new accounts).</p>
    </form>
  </div>
</template>

<style scoped>
.ava-row { display: flex; gap: 1rem; align-items: center; margin: 1rem auto; max-width: 440px; flex-wrap: wrap; justify-content: center }
.ava-lg { border-radius: 50%; border: 2px solid var(--line); object-fit: cover; background: #eee }
</style>
