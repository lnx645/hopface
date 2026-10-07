<script setup>
import { computed } from 'vue'
import { store } from '../store'
import { go } from '../router'

const me = computed(() => store.me || {})
const params = new URLSearchParams(location.search)
const loginMsg = computed(() => ({
  cancelled: 'Sign-in was cancelled.',
  failed: 'Sign-in failed. Your Google email must be verified.',
}[params.get('login')] || ''))
</script>

<template>
  <div class="card">
    <h1>Meet new people<span class="sub">on random video chat</span></h1>
    <p class="lede">Sign in with Google, pick who you want to meet, and start chatting. Free, and nothing to install.</p>

    <div v-if="me.banned" class="banner">This account is currently suspended.</div>
    <p v-if="loginMsg" class="err center">{{ loginMsg }}</p>

    <div class="center" v-if="!me.banned">
      <a v-if="me.googleEnabled" class="gbtn" href="/auth/google">
        <svg viewBox="0 0 48 48" aria-hidden="true"><path fill="#EA4335" d="M24 9.5c3.5 0 6.6 1.2 9.1 3.6l6.8-6.8C35.8 2.4 30.3 0 24 0 14.6 0 6.5 5.4 2.6 13.2l7.9 6.1C12.4 13.5 17.7 9.5 24 9.5z"/><path fill="#4285F4" d="M46.1 24.5c0-1.6-.1-3.1-.4-4.5H24v9h12.4c-.5 2.9-2.2 5.3-4.6 6.9l7.4 5.7c4.4-4 6.9-10 6.9-17.1z"/><path fill="#FBBC05" d="M10.5 28.7A14.5 14.5 0 0 1 9.5 24c0-1.6.3-3.2.8-4.7l-7.9-6.1A24 24 0 0 0 0 24c0 3.9.9 7.5 2.6 10.8l7.9-6.1z"/><path fill="#34A853" d="M24 48c6.5 0 11.9-2.1 15.9-5.8l-7.4-5.7c-2.1 1.4-4.8 2.3-8.5 2.3-6.3 0-11.6-4-13.5-9.8l-7.9 6.1C6.5 42.6 14.6 48 24 48z"/></svg>
        Sign in with Google
      </a>
      <p v-else class="muted">Google sign-in is not configured on this server yet.</p>
      <p v-if="me.dev" class="muted" style="margin-top:.8rem">
        Dev mode: <a href="/auth/dev?email=dev1@example.com&name=Dev%20One">dev1</a> ·
        <a href="/auth/dev?email=dev2@example.com&name=Dev%20Two">dev2</a> ·
        <a href="/auth/dev?email=admin@example.com&name=Admin">admin</a>
      </p>
      <p class="muted" style="margin-top:1rem">
        By signing in you confirm that you are <b>18 or older</b> and agree to the
        <a href="/terms" @click.prevent="go('/terms')">Terms</a> and
        <a href="/privacy" @click.prevent="go('/privacy')">Privacy Policy</a>.
      </p>
    </div>

    <div class="strip">
      <span><i class="icon-facetime-video"></i> Video + text</span><span><i class="icon-filter"></i> Filter by gender, age, country</span><span><i class="icon-refresh"></i> Auto-search</span><span><i class="icon-user"></i> 18+ only</span><span><i class="icon-flag"></i> Report &amp; block</span>
    </div>

    <p class="intro">
      <b>How it works.</b> Choose who you would like to meet and press <b>Start</b>. Hopface looks for someone who
      matches your filter <i>and</i> who is happy to meet you. If nobody matches within 10 seconds, your filter is
      relaxed and you are connected with anyone available. If the other person skips, you keep searching
      automatically until you press <b>Stop</b>. <b>Be respectful.</b> Nudity, harassment and anyone under 18 are not
      allowed, and every chat has a Report button.
    </p>
  </div>
</template>
