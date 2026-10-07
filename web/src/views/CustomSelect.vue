<script>
// Dideklarasikan di blok <script> biasa supaya hidup di scope modul:
// satu penghitung untuk SEMUA instance CustomSelect.
let uid = 0
export default {}
</script>

<script setup>
// Select khusus gaya 2013: tidak memakai <select> bawaan browser, sehingga tampilan dan
// daftar pilihannya konsisten di Chrome/Android/iOS. Tetap bisa diakses keyboard (tombol)
// dan membaca v-model seperti select biasa.
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'

const props = defineProps({
  modelValue: { type: String, default: '' },
  options: { type: Array, required: true }, // [{ value, label }]
  disabled: { type: Boolean, default: false },
  label: { type: String, default: '' },
})
const emit = defineEmits(['update:modelValue'])

const open = ref(false)
const root = ref(null)
// Satu siaran global saat sebuah select dibuka; select lain menutup diri,
// sehingga hanya satu dropdown yang terbuka dan user fokus ke situ.
const myId = ++uid
function onOtherOpen(e) { if (e.detail !== myId) open.value = false }
const curLabel = computed(() => {
  const o = props.options.find((o) => o.value === props.modelValue)
  return o ? o.label : (props.label || 'Select…')
})

function pick(o) {
  emit('update:modelValue', o.value)
  open.value = false
}
function toggle() {
  open.value = !open.value
  if (open.value) window.dispatchEvent(new CustomEvent('cselect-open', { detail: myId }))
}
function onDocClick(e) {
  if (root.value && !root.value.contains(e.target)) open.value = false
}
onMounted(() => { document.addEventListener('click', onDocClick); window.addEventListener('cselect-open', onOtherOpen) })
onBeforeUnmount(() => { document.removeEventListener('click', onDocClick); window.removeEventListener('cselect-open', onOtherOpen) })
</script>

<template>
  <div class="cselect" ref="root" :class="{ disabled }">
    <button type="button" class="selbase" :disabled="disabled" @click.stop="toggle" aria-haspopup="listbox" :aria-expanded="open">
      <span class="lbl">{{ curLabel }}</span>
      <i class="icon-chevron-down"></i>
    </button>
    <ul v-if="open" class="selist" role="listbox">
      <li v-for="o in options" :key="o.value" role="option" :aria-selected="o.value === modelValue"
          :class="{ sel: o.value === modelValue }" @click.stop="pick(o)">{{ o.label }}</li>
    </ul>
  </div>
</template>

<style>
/* Gaya 2013: trek abu-abu muda, batas tipis, daftar putih dengan bayangan mencolok, item hover abu muda,
   item terpilih merah tebal. Seukuran masukan (.btn/dnput). */
.cselect { position: relative }

/* Slider umur ala 2013 (jQuery UI): track bergradasi dengan bayangan dalam, dan gagang seperti tombol kecil.
   Tanpa gambar eksternal (data-URI) sehingga tidak perlu berkas tambahan. */
input[type=range] { -webkit-appearance: none; appearance: none; width: 100%; height: 26px; background: transparent; margin: .1rem 0; font: inherit }
input[type=range]::-webkit-slider-runnable-track {
  height: 8px; border: 1px solid #a0a0a0; border-radius: 4px;
  /* bagian yang sudah dilewati slider berwarna merah (0 -> --p), sisanya abu */
  background: linear-gradient(to right, #b30000 0%, #e24848 var(--p, 50%), #c9c9c9 var(--p, 50%), #efefef 100%);
  box-shadow: inset 0 1px 2px rgba(0,0,0,.2);
}
input[type=range]::-webkit-slider-thumb {
  -webkit-appearance: none; appearance: none; width: 20px; height: 20px; margin-top: -7px;
  border: 1px solid #909090; border-radius: 4px;
  background: linear-gradient(#ffffff, #d2d2d2); box-shadow: inset 0 1px 0 #fff, 0 1px 2px rgba(0,0,0,.25);
}
input[type=range]:disabled { opacity: .5 }
input[type=range]::-moz-range-track {
  height: 8px; border: 1px solid #a0a0a0; border-radius: 4px;
  background: linear-gradient(#c9c9c9, #efefef); box-shadow: inset 0 1px 2px rgba(0,0,0,.2);
}
input[type=range]::-moz-range-progress {
  height: 8px; border-radius: 4px;
  background: linear-gradient(#e24848, #b30000); box-shadow: inset 0 1px 0 rgba(255,255,255,.25);
}
input[type=range]::-moz-range-thumb {
  width: 20px; height: 20px; border: 1px solid #909090; border-radius: 4px;
  background: linear-gradient(#ffffff, #d2d2d2); box-shadow: inset 0 1px 0 #fff, 0 1px 2px rgba(0,0,0,.25);
}
input[type=number] { -moz-appearance: textfield }
input[type=number]::-webkit-inner-spin-button { -webkit-appearance: none }
.cselect .selbase {
  width: 100%; min-height: 44px; padding: .45rem .55rem; display: flex; align-items: center; justify-content: space-between; gap: .5rem;
  -webkit-appearance: none; appearance: none; text-align: left;
  border: 1px solid #a9a9a9; border-top-color: #8f8f8f; border-radius: 3px;
  background: linear-gradient(#fdfdfd, #ececec); color: var(--tx); font: inherit; font-size: 16px;
  font-family: Verdana, Tahoma, 'DejaVu Sans', Geneva, sans-serif; box-shadow: inset 0 1px 3px rgba(0,0,0,.14);
}
.cselect .selbase:not(:disabled):hover { background: linear-gradient(#ffffff, #e2e2e2) }
.cselect .selbase:focus { outline: 0; border-color: #c00; box-shadow: inset 0 1px 3px rgba(0,0,0,.14), 0 0 5px rgba(204,0,0,.4) }
.cselect.disabled .selbase { background: #efefef; color: #8a8a8a }
.cselect .selbase .icon-chevron-down { color: #5f5f5f; font-size: .8rem }
.cselect .selist {
  position: absolute; left: 0; right: 0; top: calc(100% + 3px); z-index: 60; margin: 0; padding: .2rem 0;
  list-style: none; max-height: 230px; overflow-y: auto; background: #fff;
  border: 1px solid #a9a9a9; border-radius: 3px; box-shadow: 0 4px 12px rgba(0,0,0,.28);
  font-size: 15px; font-family: Verdana, Tahoma, 'DejaVu Sans', Geneva, sans-serif;
}
.cselect .selist li { padding: .5rem .7rem; cursor: pointer; border-top: 1px solid #f0f0f0 }
.cselect .selist li:first-child { border-top: 0 }
.cselect .selist li:hover { background: #efefef; text-decoration: none }
.cselect .selist li.sel { color: var(--red); font-weight: 700 }
@media (min-width: 721px) { .cselect .selbase, .cselect .selist { font-size: .9rem; min-height: 36px } .cselect .selbase { min-height: 36px } }
</style>
