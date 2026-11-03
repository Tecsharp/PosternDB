// Theme preference: light by default, dark, or follow the OS ("system").
export type Theme = 'light' | 'dark' | 'system'

const KEY = 'posterndb-theme'

export function getTheme(): Theme {
  const v = localStorage.getItem(KEY)
  return v === 'dark' || v === 'system' ? v : 'light'
}

export function applyTheme(theme: Theme) {
  document.documentElement.dataset.theme = theme
  localStorage.setItem(KEY, theme)
}
