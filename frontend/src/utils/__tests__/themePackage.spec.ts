import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/themes', () => ({
  getActiveTheme: vi.fn(),
}))

import { getActiveTheme } from '@/api/themes'
import { loadActiveThemePackage } from '../themePackage'

const mockedGetActiveTheme = vi.mocked(getActiveTheme)

describe('loadActiveThemePackage', () => {
  beforeEach(() => {
    mockedGetActiveTheme.mockReset()
    document.head.querySelector('#sub2api-theme-package')?.remove()
    document.documentElement.removeAttribute('style')
  })

  it('applies theme tokens and stylesheet for the active package', async () => {
    mockedGetActiveTheme.mockResolvedValue({
      id: 'minimal-clean',
      name: 'Minimal Clean',
      version: '1.0.0',
      entry_css_url: '/api/v1/themes/assets/minimal-clean/theme.css',
      tokens: {
        colorPrimary500: '#2563eb',
        bgPage: '#ffffff',
      },
    })

    await loadActiveThemePackage()

    const link = document.head.querySelector<HTMLLinkElement>('#sub2api-theme-package')
    expect(link).not.toBeNull()
    expect(link?.href).toContain('/api/v1/themes/assets/minimal-clean/theme.css?v=1.0.0')
    expect(document.documentElement.style.getPropertyValue('--s2a-color-primary500')).toBe('#2563eb')
    expect(document.documentElement.style.getPropertyValue('--s2a-bg-page')).toBe('#ffffff')
  })

  it('clears prior theme tokens and stylesheet when no active package exists', async () => {
    mockedGetActiveTheme.mockResolvedValueOnce({
      id: 'minimal-clean',
      name: 'Minimal Clean',
      version: '1.0.0',
      entry_css_url: '/api/v1/themes/assets/minimal-clean/theme.css',
      tokens: {
        colorPrimary500: '#2563eb',
      },
    })
    await loadActiveThemePackage()

    mockedGetActiveTheme.mockResolvedValueOnce(null)
    await loadActiveThemePackage()

    expect(document.head.querySelector('#sub2api-theme-package')).toBeNull()
    expect(document.documentElement.style.getPropertyValue('--s2a-color-primary500')).toBe('')
  })
})
