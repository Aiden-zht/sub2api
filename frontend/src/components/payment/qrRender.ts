export type QRRenderKind = 'image' | 'canvas'

const IMAGE_EXTENSION_RE = /\.(png|jpe?g|gif|webp|svg)(?:[?#].*)?$/i

export function getQRRenderKind(value: string): QRRenderKind {
  const trimmed = value.trim()
  if (!trimmed) return 'canvas'
  if (trimmed.startsWith('data:image/')) return 'image'
  if (!/^https?:\/\//i.test(trimmed)) return 'canvas'

  try {
    const url = new URL(trimmed)
    const path = url.pathname.toLowerCase()
    if (IMAGE_EXTENSION_RE.test(path)) return 'image'
    if (path.includes('qrcode') || path.includes('qr_code') || path.includes('/qr/')) return 'image'
  } catch {
    return 'canvas'
  }

  return 'canvas'
}

export function isQRImage(value: string): boolean {
  return getQRRenderKind(value) === 'image'
}
