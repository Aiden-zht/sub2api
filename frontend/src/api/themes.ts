import { apiClient } from './client'

export interface ActiveTheme {
  id: string
  name: string
  version: string
  entry_css_url: string
  tokens?: Record<string, string>
}

export interface ThemePackage {
  id: string
  name: string
  version: string
  enabled: boolean
  installed_at: string
  updated_at: string
  manifest: {
    id: string
    name: string
    version: string
    sub2apiThemeApi?: string
    author?: string
    description?: string
    entry: string
    assets?: Record<string, string>
    tokens?: Record<string, string>
    capabilities?: string[]
  }
}

export async function getActiveTheme(): Promise<ActiveTheme | null> {
  const { data } = await apiClient.get<ActiveTheme | null>('/themes/active')
  return data
}

export const adminThemesAPI = {
  async list(): Promise<{ themes: ThemePackage[] }> {
    const { data } = await apiClient.get<{ themes: ThemePackage[] }>('/admin/themes')
    return data
  },

  async upload(file: File): Promise<ThemePackage> {
    const form = new FormData()
    form.append('file', file)
    const { data } = await apiClient.post<ThemePackage>('/admin/themes/upload', form)
    return data
  },

  async enable(id: string): Promise<void> {
    await apiClient.post(`/admin/themes/${encodeURIComponent(id)}/enable`)
  },

  async disable(id: string): Promise<void> {
    await apiClient.post(`/admin/themes/${encodeURIComponent(id)}/disable`)
  },

  async delete(id: string): Promise<void> {
    await apiClient.delete(`/admin/themes/${encodeURIComponent(id)}`)
  },
}
