import { describe, expect, it } from 'vitest'
import { getQRRenderKind, isQRImage } from '../qrRender'

describe('qrRender', () => {
  it('renders image URLs from hosted QR providers as images', () => {
    expect(getQRRenderKind('https://api.xunhupay.com/qrcode/ORDER123.png')).toBe('image')
    expect(getQRRenderKind('https://pay.example.com/payment/url_qrcode?order=ORDER123')).toBe('image')
    expect(isQRImage('data:image/png;base64,abc')).toBe(true)
  })

  it('keeps native payment payloads and mobile checkout URLs on canvas', () => {
    expect(getQRRenderKind('weixin://wxpay/bizpayurl?pr=abc')).toBe('canvas')
    expect(getQRRenderKind('alipayqr://platformapi/startapp?saId=10000007')).toBe('canvas')
    expect(getQRRenderKind('https://api.xunhupay.com/payments/wechat/index?hash=abc')).toBe('canvas')
  })
})
