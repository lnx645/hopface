// Router mini berbasis History API. Server Go mengembalikan index.html untuk path tanpa ekstensi.
import { reactive } from 'vue'

export const route = reactive({ path: location.pathname })

export function go(path) {
  if (path === route.path) return
  history.pushState(null, '', path)
  route.path = path
  window.scrollTo(0, 0)
}

addEventListener('popstate', () => { route.path = location.pathname })
