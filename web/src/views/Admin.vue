<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../store'

const reports = ref([])
const error = ref('')
const msg = ref('')

async function load() {
  const r = await api('GET', '/api/admin/reports')
  if (r.ok) reports.value = r.data.reports || []
  else error.value = 'Could not load reports.'
}

async function ban(rep, hours) {
  const r = await api('POST', '/api/admin/ban', { userId: rep.reportedId, reason: rep.reason, hours })
  msg.value = r.ok ? `Banned ${rep.reportedId}${hours ? ` for ${hours}h` : ' permanently'}.` : 'Ban failed.'
}

async function handle(rep) {
  const r = await api('POST', `/api/admin/reports/${rep.id}/handle`)
  if (r.ok) rep.handled = true
}

const fmt = (t) => new Date(t).toLocaleString()
onMounted(load)
</script>

<template>
  <div class="card">
    <h2><i class="icon-flag"></i> Reports</h2>
    <p v-if="error" class="err">{{ error }}</p>
    <p v-if="msg" class="ok">{{ msg }}</p>
    <p v-if="!reports.length && !error" class="muted">No reports yet.</p>
    <div class="tbl-wrap">
      <table v-if="reports.length" class="tbl">
        <thead><tr><th>When</th><th>Reported user</th><th>Reason</th><th>Evidence</th><th>Last messages</th><th>Actions</th></tr></thead>
        <tbody>
          <tr v-for="r in reports" :key="r.id" :style="r.handled ? 'opacity:.5' : ''">
            <td>{{ fmt(r.at) }}</td>
            <td>{{ r.reportedId }}<br><span class="muted">by {{ r.reporterId }}</span></td>
            <td>{{ r.reason }}</td>
            <td><img v-if="r.frame" class="evidence" :src="r.frame" alt="Reported frame" loading="lazy">
                <span v-else class="muted">—</span></td>
            <td><pre>{{ (r.transcript || []).join('\n') || '—' }}</pre></td>
            <td>
              <button class="btn small" type="button" @click="ban(r, 24)">Ban 24h</button>
              <button class="btn red small" type="button" @click="ban(r, 0)">Ban forever</button>
              <button class="btn small" type="button" :disabled="r.handled" @click="handle(r)">Mark handled</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style>
/* Bukti frame laporan: thumbnail 2013, klik untuk melihat ukuran asli. */
.evidence { width: 96px; height: auto; border: 1px solid var(--line-2); box-shadow: 0 1px 2px rgba(0,0,0,.15); cursor: pointer }
</style>
