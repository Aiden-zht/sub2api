import { getActiveTheme, type ActiveTheme } from '@/api/themes'

const THEME_LINK_ID = 'sub2api-theme-package'
const TOKEN_PREFIX = '--s2a-'

function tokenNameToCSSVar(name: string): string {
  return TOKEN_PREFIX + name.replace(/([a-z0-9])([A-Z])/g, '$1-$2').replace(/_/g, '-').toLowerCase()
}

function clearThemeTokens(previous?: Record<string, string>): void {
  if (!previous) return
  for (const key of Object.keys(previous)) {
    document.documentElement.style.removeProperty(tokenNameToCSSVar(key))
  }
}

function applyThemeTokens(tokens?: Record<string, string>): void {
  if (!tokens) return
  for (const [key, value] of Object.entries(tokens)) {
    document.documentElement.style.setProperty(tokenNameToCSSVar(key), value)
  }
}

function setThemeStylesheet(theme: ActiveTheme | null): void {
  const current = document.getElementById(THEME_LINK_ID) as HTMLLinkElement | null
  if (!theme?.entry_css_url) {
    current?.remove()
    return
  }
  const href = `${theme.entry_css_url}${theme.entry_css_url.includes('?') ? '&' : '?'}v=${encodeURIComponent(theme.version)}`
  if (current) {
    if (current.href !== new URL(href, window.location.origin).href) {
      current.href = href
    }
    return
  }
  const link = document.createElement('link')
  link.id = THEME_LINK_ID
  link.rel = 'stylesheet'
  link.href = href
  document.head.appendChild(link)
}

let appliedTokens: Record<string, string> | undefined

export async function loadActiveThemePackage(): Promise<ActiveTheme | null> {
  try {
    const theme = await getActiveTheme()
    clearThemeTokens(appliedTokens)
    appliedTokens = theme?.tokens
    applyThemeTokens(theme?.tokens)
    setThemeStylesheet(theme)
    return theme
  } catch (error) {
    console.error('Failed to load active theme package:', error)
    return null
  }
}

export async function refreshActiveThemePackage(): Promise<ActiveTheme | null> {
  return loadActiveThemePackage()
}
