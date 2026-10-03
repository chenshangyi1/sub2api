import { describe, expect, it } from 'vitest'

import { cnSupportsNativeResponses, defaultCNAdaptiveBaseUrls, deriveCNAdaptiveBaseUrlsFromPrimary, isGeminiOpenAIProtocolAccount, resolveCNAdaptiveBaseUrls } from '../credentialsBuilder'

describe('defaultCNAdaptiveBaseUrls', () => {
  it('resolves Kimi endpoints by account mode', () => {
    expect(defaultCNAdaptiveBaseUrls('kimi', 'payg')).toEqual({
      chat_completions: 'https://api.moonshot.cn/v1',
      anthropic: 'https://api.moonshot.cn/anthropic',
      responses: 'https://api.moonshot.cn/v1'
    })
    expect(defaultCNAdaptiveBaseUrls('kimi', 'coding')).toEqual({
      chat_completions: 'https://api.kimi.com/coding/v1',
      anthropic: 'https://api.kimi.com/coding',
      responses: 'https://api.kimi.com/coding/v1'
    })
  })

  it('resolves GLM endpoints by account mode', () => {
    expect(defaultCNAdaptiveBaseUrls('zhipu', 'payg')).toEqual({
      chat_completions: 'https://open.bigmodel.cn/api/paas/v4',
      anthropic: 'https://open.bigmodel.cn/api/anthropic',
      responses: ''
    })
    expect(defaultCNAdaptiveBaseUrls('zhipu', 'coding')).toEqual({
      chat_completions: 'https://open.bigmodel.cn/api/coding/paas/v4',
      anthropic: 'https://open.bigmodel.cn/api/anthropic',
      responses: ''
    })
  })

  it('includes all three native DeepSeek endpoints', () => {
    expect(defaultCNAdaptiveBaseUrls('deepseek', 'payg')).toEqual({
      chat_completions: 'https://api.deepseek.com',
      anthropic: 'https://api.deepseek.com/anthropic',
      responses: 'https://api.deepseek.com'
    })
  })

  it('includes MiniMax official chat, anthropic, and responses endpoints', () => {
    expect(defaultCNAdaptiveBaseUrls('minimax', 'payg')).toEqual({
      chat_completions: 'https://api.minimaxi.com/v1',
      anthropic: 'https://api.minimaxi.com/anthropic',
      responses: 'https://api.minimaxi.com/v1'
    })
    expect(defaultCNAdaptiveBaseUrls('minimax', 'coding')).toEqual({
      chat_completions: 'https://api.minimaxi.com/v1',
      anthropic: 'https://api.minimaxi.com/anthropic',
      responses: 'https://api.minimaxi.com/v1'
    })
  })
})

describe('deriveCNAdaptiveBaseUrlsFromPrimary', () => {
  it('fills empty protocol slots from a custom primary relay', () => {
    expect(deriveCNAdaptiveBaseUrlsFromPrimary('deepseek', 'payg', 'http://51.161.119.83:17777')).toEqual({
      chat_completions: 'http://51.161.119.83:17777',
      anthropic: 'http://51.161.119.83:17777',
      responses: 'http://51.161.119.83:17777'
    })
  })

  it('keeps official protocol defaults when the primary URL is the official chat endpoint', () => {
    expect(deriveCNAdaptiveBaseUrlsFromPrimary('deepseek', 'payg', 'https://api.deepseek.com')).toEqual({
      chat_completions: 'https://api.deepseek.com',
      anthropic: 'https://api.deepseek.com/anthropic',
      responses: 'https://api.deepseek.com'
    })
  })
})

describe('resolveCNAdaptiveBaseUrls', () => {
  it('copies a custom relay URL into empty Anthropic and Responses slots', () => {
    expect(resolveCNAdaptiveBaseUrls('deepseek', 'payg', {}, 'http://51.161.119.83:17777')).toEqual({
      chat_completions: 'http://51.161.119.83:17777',
      anthropic: 'http://51.161.119.83:17777',
      responses: 'http://51.161.119.83:17777'
    })
  })

  it('keeps official vendor defaults when the chat URL is the official one', () => {
    expect(resolveCNAdaptiveBaseUrls('deepseek', 'payg', {}, 'https://api.deepseek.com')).toEqual({
      chat_completions: 'https://api.deepseek.com',
      anthropic: 'https://api.deepseek.com/anthropic',
      responses: 'https://api.deepseek.com'
    })
  })

  it('does not overwrite an explicit custom Anthropic endpoint', () => {
    expect(resolveCNAdaptiveBaseUrls('deepseek', 'payg', {
      chat_completions: 'http://relay.example.com',
      anthropic: 'http://relay.example.com/anthropic'
    }, 'http://relay.example.com')).toEqual({
      chat_completions: 'http://relay.example.com',
      anthropic: 'http://relay.example.com/anthropic',
      responses: 'http://relay.example.com'
    })
  })

  it('replaces official default Anthropic/Responses slots when the chat URL is a custom relay', () => {
    expect(resolveCNAdaptiveBaseUrls('deepseek', 'payg', {
      chat_completions: 'http://relay.example.com',
      anthropic: 'https://api.deepseek.com/anthropic',
      responses: 'https://api.deepseek.com'
    }, 'http://relay.example.com')).toEqual({
      chat_completions: 'http://relay.example.com',
      anthropic: 'http://relay.example.com',
      responses: 'http://relay.example.com'
    })
  })
})

describe('cnSupportsNativeResponses', () => {
  it('covers DeepSeek, Kimi, and Gemini custom upstreams', () => {
    expect(cnSupportsNativeResponses('deepseek')).toBe(true)
    expect(cnSupportsNativeResponses('kimi')).toBe(true)
    expect(cnSupportsNativeResponses('minimax')).toBe(true)
    expect(cnSupportsNativeResponses('gemini')).toBe(true)
    expect(cnSupportsNativeResponses('zhipu')).toBe(false)
  })
})

describe('isGeminiOpenAIProtocolAccount', () => {
  it('treats Gemini API keys with adaptive protocol as custom upstream', () => {
    expect(isGeminiOpenAIProtocolAccount({
      platform: 'gemini',
      type: 'apikey',
      credentials: { api_protocol: 'responses' }
    })).toBe(true)
    expect(isGeminiOpenAIProtocolAccount({
      platform: 'gemini',
      type: 'apikey',
      credentials: {}
    })).toBe(false)
  })
})
