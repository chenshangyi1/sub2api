import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import PaymentProviderDialog from '@/components/payment/PaymentProviderDialog.vue'
import { STRIPE_SDK_API_VERSION } from '@/components/payment/providerConfig'
import type { ProviderInstance } from '@/types/payment'

const messages: Record<string, string> = {
  'admin.settings.payment.providerConfig': 'Credentials',
  'admin.settings.payment.easypayCustomMethods': 'Custom EasyPay methods',
  'admin.settings.payment.easypayCustomMethodsHint': 'Add provider-specific EasyPay type values.',
  'admin.settings.payment.addCustomMethod': 'Add method',
  'admin.settings.payment.customMethodType': 'Payment type',
  'admin.settings.payment.customMethodUpstreamType': 'Upstream type',
  'admin.settings.payment.customMethodDisplayName': 'Display name',
  'admin.settings.payment.customMethodDisplayNamePlaceholder': '信用卡',
  'admin.settings.payment.paymentGuideTrigger': 'View payment guide',
  'admin.settings.payment.alipayGuideSummary': 'Desktop prefers QR precreate and falls back to cashier; mobile prefers WAP checkout.',
  'admin.settings.payment.wxpayGuideSummary': 'Desktop prefers Native QR; mobile routes to JSAPI or H5 based on browser context.',
  'admin.settings.payment.airwallexGuideSummary': 'Use Payment Acceptance read/write only.',
  'admin.settings.payment.stripeWebhookHint': 'Configure Stripe webhook.',
  'admin.settings.payment.stripeWebhookApiVersionHint': 'Use Stripe API version {version}.',
  'admin.settings.payment.airwallexWebhookHint': 'Select payment_intent.succeeded and use the latest stable API version.',
}

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, string>) => {
      const message = messages[key] ?? key
      if (!params) return message
      return Object.entries(params).reduce(
        (value, [name, replacement]) => value.replaceAll(`{${name}}`, replacement),
        message,
      )
    },
  }),
}))

function providerFactory(overrides: Partial<ProviderInstance> = {}): ProviderInstance {
  return {
    id: 1,
    provider_key: 'airwallex',
    name: 'Airwallex',
    config: {},
    supported_types: ['airwallex'],
    enabled: true,
    payment_mode: '',
    refund_enabled: false,
    allow_user_refund: false,
    limits: '',
    sort_order: 0,
    ...overrides,
  }
}

function mountDialog(options: { editing?: ProviderInstance | null } = {}) {
  return mount(PaymentProviderDialog, {
    props: {
      show: true,
      saving: false,
      editing: options.editing ?? null,
      allKeyOptions: [
        { value: 'easypay', label: 'EasyPay' },
        { value: 'alipay', label: 'Alipay' },
        { value: 'wxpay', label: 'WeChat Pay' },
        { value: 'stripe', label: 'Stripe' },
        { value: 'airwallex', label: 'Airwallex' },
        { value: 'epusdt', label: 'EPUSDT' },
      ],
      enabledKeyOptions: [
        { value: 'easypay', label: 'EasyPay' },
        { value: 'alipay', label: 'Alipay' },
        { value: 'wxpay', label: 'WeChat Pay' },
        { value: 'airwallex', label: 'Airwallex' },
        { value: 'epusdt', label: 'EPUSDT' },
      ],
      allPaymentTypes: [
        { value: 'alipay', label: 'Alipay' },
        { value: 'wxpay', label: 'WeChat Pay' },
      ],
      redirectLabel: 'Redirect',
    },
    global: {
      stubs: {
        BaseDialog: {
          template: '<div><slot /><slot name="footer" /></div>',
        },
        Select: {
          props: ['modelValue', 'options', 'disabled'],
          template: '<div />',
        },
        ToggleSwitch: {
          template: '<div />',
        },
      },
    },
  })
}

describe('PaymentProviderDialog payment guide', () => {
  it('shows no payment guide for providers without a flow guide', () => {
    const wrapper = mountDialog()

    expect(wrapper.text()).not.toContain(messages['admin.settings.payment.alipayGuideSummary'])
    expect(wrapper.text()).not.toContain(messages['admin.settings.payment.wxpayGuideSummary'])
    expect(wrapper.find('button[title="View payment guide"]').exists()).toBe(false)
  })

  it.each([
    ['alipay', 'admin.settings.payment.alipayGuideSummary'],
    ['wxpay', 'admin.settings.payment.wxpayGuideSummary'],
    ['airwallex', 'admin.settings.payment.airwallexGuideSummary'],
  ])('shows the payment guide summary for %s', async (providerKey, summaryKey) => {
    const wrapper = mountDialog()

    ;(wrapper.vm as unknown as { reset: (key: string) => void }).reset(providerKey)
    await nextTick()

    expect(wrapper.text()).toContain(messages[summaryKey])
    expect(wrapper.find('button[title="View payment guide"]').exists()).toBe(true)
  })

  it('shows Airwallex webhook event and API version guidance with the webhook URL', async () => {
    const wrapper = mountDialog()

    ;(wrapper.vm as unknown as { reset: (key: string) => void }).reset('airwallex')
    await nextTick()

    expect(wrapper.text()).toContain(messages['admin.settings.payment.airwallexWebhookHint'])
    expect(wrapper.text()).toContain('/api/v1/payment/webhook/airwallex')
  })

  it('shows Stripe webhook API version guidance with the integrated SDK version', async () => {
    const wrapper = mountDialog()

    ;(wrapper.vm as unknown as { reset: (key: string) => void }).reset('stripe')
    await nextTick()

    expect(wrapper.text()).toContain(messages['admin.settings.payment.stripeWebhookHint'])
    expect(wrapper.text()).toContain(`Use Stripe API version ${STRIPE_SDK_API_VERSION}.`)
    expect(wrapper.text()).toContain('/api/v1/payment/webhook/stripe')
  })

  it('emits an empty Airwallex accountId when the admin clears it', async () => {
    const provider = providerFactory({
      config: {
        clientId: 'cid_123',
        apiBase: 'https://api.airwallex.com/api/v1',
        countryCode: 'CN',
        currency: 'CNY',
        accountId: 'acct_123',
      },
    })
    const wrapper = mountDialog({ editing: provider })

    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()

    const accountIdInput = wrapper
      .findAll('input[type="text"]')
      .find(input => (input.element as HTMLInputElement).value === 'acct_123')
    if (!accountIdInput) throw new Error('accountId input not found')

    await accountIdInput.setValue('')
    await wrapper.find('form').trigger('submit.prevent')

    const payload = wrapper.emitted('save')?.[0]?.[0] as { config: Record<string, string> }
    expect(payload.config.accountId).toBe('')
  })

  it.each(['epay', 'usdt.trc20'])('serializes EasyPay upstream type %s and adds the local type to supported_types', async (upstreamType) => {
    const provider = providerFactory({
      provider_key: 'easypay',
      name: 'EasyPay',
      config: {
        pid: 'pid-1',
        apiBase: 'https://pay.example.com',
        notifyUrl: 'https://example.com/api/v1/payment/webhook/easypay',
        returnUrl: 'https://example.com/payment/result',
      },
      supported_types: ['alipay', 'wxpay'],
      payment_mode: 'qrcode',
    })
    const wrapper = mountDialog({ editing: provider })

    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()

    await wrapper.find('button.btn-sm').trigger('click')
    await nextTick()

    const inputs = wrapper.findAll('input[type="text"]')
    const customTypeInputs = inputs.filter(input => (input.element as HTMLInputElement).placeholder === 'credit_card')
    const ldcTypeInput = customTypeInputs[0]
    const upstreamTypeInput = customTypeInputs[1]
    const displayNameInput = inputs.find(input => (input.element as HTMLInputElement).placeholder === '信用卡')
    if (!ldcTypeInput || !upstreamTypeInput || !displayNameInput) {
      throw new Error('custom method inputs not found')
    }

    await ldcTypeInput.setValue('ldc')
    await upstreamTypeInput.setValue(upstreamType)
    await displayNameInput.setValue('LDC')
    await wrapper.find('form').trigger('submit.prevent')

    const payload = wrapper.emitted('save')?.[0]?.[0] as {
      config: Record<string, string>
      supported_types: string[]
    }
    expect(JSON.parse(payload.config.customMethods)).toEqual([{ type: 'ldc', upstreamType, displayName: 'LDC' }])
    expect(payload.supported_types).toEqual(['alipay', 'wxpay', 'ldc'])
  })

  it('saves multiple EPUSDT networks as a comma-separated config value', async () => {
    const provider = providerFactory({
      provider_key: 'epusdt',
      name: 'USDT',
      supported_types: ['epusdt'],
      config: {
        pid: '1000',
        apiBase: 'https://ep.baiyuan.cc.cd',
        token: 'USDT',
        network: 'bsc',
        currency: 'CNY',
        notifyUrl: 'https://example.com/api/v1/payments/callback',
        returnUrl: 'https://example.com/payment/result',
      },
    })
    const wrapper = mountDialog({ editing: provider })
    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()

    const trc20 = wrapper.find('[data-testid="epusdt-network-trc20"]')
    const erc20 = wrapper.find('[data-testid="epusdt-network-erc20"]')
    if (!trc20.exists() || !erc20.exists()) {
      throw new Error('EPUSDT network chips not found')
    }
    await trc20.trigger('click')
    await erc20.trigger('click')
    await wrapper.find('form').trigger('submit.prevent')

    const payload = wrapper.emitted('save')?.[0]?.[0] as { config: Record<string, string> }
    expect(payload.config.networks.split(',').sort()).toEqual(['bsc', 'erc20', 'trc20'])
    expect(payload.config.network).toBe('bsc')
  })

  it.each(['alipay', 'wxpay', 'epusdt'])('loads and saves per-instance recharge fee fields for %s', async (providerKey) => {
    const provider = providerFactory({
      provider_key: providerKey,
      name: `${providerKey} merchant`,
      supported_types: [providerKey],
      recharge_fee_rate: 1.5,
      balance_recharge_multiplier: 0.14,
      config: providerKey === 'epusdt'
        ? {
            pid: 'pid-1',
            apiBase: 'https://ep.kedaya.xyz',
            token: 'USDT',
            network: 'bsc',
            currency: 'CNY',
          }
        : providerKey === 'alipay'
          ? { appId: 'app-1' }
          : {
              appId: 'wx-app',
              mchId: 'mch-1',
              certSerial: 'serial-1',
              publicKeyId: 'pubkey-1',
            },
    })
    const wrapper = mountDialog({ editing: provider })

    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()

    const feeInput = wrapper.get('[data-testid="instance-recharge-fee-rate"]')
    const multiplierInput = wrapper.get('[data-testid="instance-balance-recharge-multiplier"]')
    expect((feeInput.element as HTMLInputElement).value).toBe('1.5')
    expect((multiplierInput.element as HTMLInputElement).value).toBe('0.14')

    await feeInput.setValue('2.5')
    await multiplierInput.setValue('0.2')
    await wrapper.find('form').trigger('submit.prevent')

    const payload = wrapper.emitted('save')?.[0]?.[0] as {
      recharge_fee_rate: number
      balance_recharge_multiplier: number
    }
    expect(payload.recharge_fee_rate).toBe(2.5)
    expect(payload.balance_recharge_multiplier).toBe(0.2)
  })

  it('clears per-instance recharge fee override when the field is emptied', async () => {
    const provider = providerFactory({
      provider_key: 'epusdt',
      name: 'USDT',
      supported_types: ['epusdt'],
      recharge_fee_rate: 3,
      config: {
        pid: 'pid-1',
        apiBase: 'https://ep.kedaya.xyz',
        token: 'USDT',
        network: 'bsc',
        currency: 'CNY',
      },
    })
    const wrapper = mountDialog({ editing: provider })
    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()

    await wrapper.get('[data-testid="instance-recharge-fee-rate"]').setValue('')
    await wrapper.find('form').trigger('submit.prevent')

    const payload = wrapper.emitted('save')?.[0]?.[0] as {
      recharge_fee_rate: number | null
    }
    expect(payload.recharge_fee_rate).toBeNull()
  })

  it('rejects custom EasyPay method types with built-in payment prefixes', async () => {
    const provider = providerFactory({
      provider_key: 'easypay',
      name: 'EasyPay',
      config: {
        pid: 'pid-1',
        apiBase: 'https://pay.example.com',
        notifyUrl: 'https://example.com/api/v1/payment/webhook/easypay',
        returnUrl: 'https://example.com/payment/result',
      },
      supported_types: ['alipay', 'wxpay'],
      payment_mode: 'qrcode',
    })
    const wrapper = mountDialog({ editing: provider })

    ;(wrapper.vm as unknown as { loadProvider: (provider: ProviderInstance) => void }).loadProvider(provider)
    await nextTick()

    await wrapper.find('button.btn-sm').trigger('click')
    await nextTick()

    const inputs = wrapper.findAll('input[type="text"]')
    const customTypeInputs = inputs.filter(input => (input.element as HTMLInputElement).placeholder === 'credit_card')
    const typeInput = customTypeInputs[0]
    const upstreamTypeInput = customTypeInputs[1]
    const displayNameInput = inputs.find(input => (input.element as HTMLInputElement).placeholder === '信用卡')
    if (!typeInput || !upstreamTypeInput || !displayNameInput) {
      throw new Error('custom method inputs not found')
    }

    await typeInput.setValue(type)
    await upstreamTypeInput.setValue(upstreamType)
    await displayNameInput.setValue('Custom payment')
    await wrapper.find('form').trigger('submit.prevent')

    expect(wrapper.emitted('save')).toBeUndefined()
  })
})