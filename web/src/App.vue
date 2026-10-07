<script setup>
import { computed, onMounted, ref } from 'vue'
import { route, go } from './router'
import { store, loadMe, logout } from './store'
import Landing from './views/Landing.vue'
import Profile from './views/Profile.vue'
import Chat from './views/Chat.vue'
import Admin from './views/Admin.vue'
import Terms from './views/Terms.vue'
import Privacy from './views/Privacy.vue'

onMounted(loadMe)

const me = computed(() => store.me)
const sheet = ref(false)

// Halaman yang ditampilkan ditentukan oleh path dan status login/profil.
const view = computed(() => {
  const p = route.path.replace(/\/+$/, '') || '/'
  if (p === '/terms') return Terms
  if (p === '/privacy') return Privacy
  if (!me.value || !me.value.authenticated) return Landing
  if (me.value.banned) return Landing
  // Profil selalu bisa dibuka; bila belum lengkap, ini juga dipaksa dulu saat route '/'.
  if (p === '/profile' || !me.value.profileComplete) return Profile
  if (p === '/admin' && me.value.admin) return Admin
  return Chat
})

function onProfileDone() { loadMe() }
</script>

<template>
  <header class="top">
    <div class="shell hd">
      <a class="brand" href="/" @click.prevent="go('/')">
        <img src="/logo.svg" alt="Hopface" width="24" height="24"> <span class="bname">Hopface</span> <span class="beta">beta</span>
      </a>
      <nav class="nav">
        <a href="/terms" @click.prevent="go('/terms')"><i class="icon-file-alt"></i> Terms</a>
        <a href="/privacy" @click.prevent="go('/privacy')"><i class="icon-lock"></i> Privacy</a>
        <template v-if="me && me.authenticated">
          <a v-if="me.admin" href="/admin" @click.prevent="go('/admin')"><i class="icon-cog"></i> Admin</a>
          <span class="who">{{ me.user.name }}</span>
          <button class="link" type="button" @click="logout"><i class="icon-signout"></i> Sign out</button>
        </template>
      </nav>
      <button class="menubtn btn" type="button" @click="sheet = true" aria-label="Open menu">
        <i class="icon-list"></i> Menu
      </button>
    </div>
  </header>

  <main>
    <div class="shell">
      <div v-if="store.loading" class="card center muted"><img src="/img/ajax-loader.gif" alt="" width="28" height="28" style="margin:0 auto .4rem">Loading…</div>
      <div v-else-if="store.failed" class="card center">
        <p class="err">Could not reach the server.</p>
        <button class="btn" type="button" @click="loadMe">Try again</button>
      </div>
      <component v-else :is="view" @done="onProfileDone" />
    </div>
  </main>

  <!-- Bottom sheet gaya "web 2013": diminta untuk HP; desktop tetap memakai nav di header. -->
  <div v-if="sheet" class="backdrop" @click="sheet = false"></div>
  <div v-if="sheet" class="sheet" role="dialog" aria-label="Menu">
    <div class="grab"></div>
    <nav class="snav">
      <a href="/" @click.prevent="go('/'); sheet = false"><i class="icon-home"></i> Home</a>
      <a href="/profile" @click.prevent="go('/profile'); sheet = false"><i class="icon-user"></i> Profile</a>
      <a href="/terms" @click.prevent="go('/terms'); sheet = false"><i class="icon-file-alt"></i> Terms</a>
      <a href="/privacy" @click.prevent="go('/privacy'); sheet = false"><i class="icon-lock"></i> Privacy</a>
      <template v-if="me && me.authenticated">
        <a v-if="me.admin" href="/admin" @click.prevent="go('/admin'); sheet = false"><i class="icon-cog"></i> Admin</a>
        <a href="#" @click.prevent="sheet = false; logout()"><i class="icon-signout"></i> Sign out</a>
        <p class="muted center" style="padding:.5rem 0 .1rem">{{ me.user.name }} · {{ me.user.email }}</p>
      </template>
    </nav>
    <button class="btn" type="button" style="width:100%;margin-top:.6rem" @click="sheet = false">Close</button>
  </div>

  <footer class="bottom">
    <i class="icon-info-sign"></i> Hopface is for adults (18+) only. Not affiliated with any other chat service.
    <div class="navfoot"><a href="/terms" @click.prevent="go('/terms')"><i class="icon-file-alt"></i> Terms</a> · <a href="/privacy" @click.prevent="go('/privacy')"><i class="icon-lock"></i> Privacy</a></div>
  </footer>
</template>
