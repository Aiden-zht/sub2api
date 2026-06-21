import { describe, expect, it } from 'vitest'
import { PAYMENT_CURRENCY_OPTIONS, PROVIDER_CALLBACK_PATHS, PROVIDER_CONFIG_FIELDS, PROVIDER_SUPPORTED_TYPES, getDefaultNotifyBaseUrl, isProviderEnabledForPaymentTypes, providerSupportsRefund } from '@/components/payment/providerConfig'

function findField(providerKey: string, key: string) {
  const fields = PROVIDER_CONFIG_FIELDS[providerKey] || []
  return fields.find(field => field.key === key)
}

describe('PROVIDER_CONFIG_FIELDS.wxpay', () => {
  it('keeps admin form validation aligned with backend-required credentials', () => {
    expect(findField('wxpay', 'publicKeyId')?.optional).toBeFalsy()
    expect(findField('wxpay', 'certSerial')?.optional).toBeFalsy()
  })

  it('only keeps the simplified visible credential set in the admin form', () => {
    expect(findField('wxpay', 'mpAppId')).toBeUndefined()
    expect(findField('wxpay', 'h5AppName')).toBeUndefined()
    expect(findField('wxpay', 'h5AppUrl')).toBeUndefined()
  })
})

describe('PROVIDER_CONFIG_FIELDS.airwallex', () => {
  it('adds currency config with CNY as the default', () => {
    const currency = findField('airwallex', 'currency')

    expect(currency?.defaultValue).toBe('CNY')
    expect(currency?.hintKey).toBe('admin.settings.payment.field_paymentCurrencyHint')
    expect(currency?.options).toBe(PAYMENT_CURRENCY_OPTIONS)
  })

  it('marks accountId as optional and explains when it can be left blank', () => {
    const accountId = findField('airwallex', 'accountId')

    expect(accountId?.optional).toBe(true)
    expect(accountId?.clearable).toBe(true)
    expect(accountId?.hintKey).toBe('admin.settings.payment.field_accountIdHint')
  })

  it('explains that apiBase must match the Airwallex key environment', () => {
    expect(findField('airwallex', 'apiBase')?.hintKey).toBe('admin.settings.payment.field_airwallexApiBaseHint')
  })
})

describe('PROVIDER_CONFIG_FIELDS.stripe', () => {
  it('adds currency config with CNY as the default', () => {
    const currency = findField('stripe', 'currency')

    expect(currency?.defaultValue).toBe('CNY')
    expect(currency?.hintKey).toBe('admin.settings.payment.field_paymentCurrencyHint')
    expect(currency?.options).toBe(PAYMENT_CURRENCY_OPTIONS)
  })
})

describe('PROVIDER_CONFIG_FIELDS.xunhupay', () => {
  it('supports Alipay and WeChat with a first-class webhook path', () => {
    expect(PROVIDER_SUPPORTED_TYPES.xunhupay).toEqual(['alipay', 'wxpay'])
    expect(PROVIDER_CALLBACK_PATHS.xunhupay?.notifyUrl).toBe('/api/v1/payment/webhook/xunhupay')
  })

  it('marks appSecret as sensitive and apiBase as visible', () => {
    expect(findField('xunhupay', 'appSecret')?.sensitive).toBe(true)
    expect(findField('xunhupay', 'apiBase')?.sensitive).toBe(false)
  })
})

describe('isProviderEnabledForPaymentTypes', () => {
  it('shows aggregate providers when their visible payment methods are enabled', () => {
    expect(isProviderEnabledForPaymentTypes('xunhupay', ['alipay'])).toBe(true)
    expect(isProviderEnabledForPaymentTypes('xunhupay', ['wxpay'])).toBe(true)
    expect(isProviderEnabledForPaymentTypes('easypay', ['alipay'])).toBe(true)
  })

  it('keeps direct providers tied to their own payment type', () => {
    expect(isProviderEnabledForPaymentTypes('alipay', ['alipay'])).toBe(true)
    expect(isProviderEnabledForPaymentTypes('wxpay', ['alipay'])).toBe(false)
    expect(isProviderEnabledForPaymentTypes('xunhupay', ['stripe'])).toBe(false)
  })
})

describe('getDefaultNotifyBaseUrl', () => {
  it('uses the backend dev port when the admin UI is served by Vite', () => {
    expect(getDefaultNotifyBaseUrl('http://192.168.0.203:3000')).toBe('http://192.168.0.203:8080')
  })

  it('prefers an absolute API base URL when configured', () => {
    expect(getDefaultNotifyBaseUrl('http://app.example.com', 'https://api.example.com/api/v1')).toBe('https://api.example.com')
  })
})

describe('providerSupportsRefund', () => {
  it('treats XunhuPay as refund-capable once refund parity is implemented', () => {
    expect(providerSupportsRefund('xunhupay')).toBe(true)
  })
})
