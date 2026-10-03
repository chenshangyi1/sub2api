import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'

import BrandLogo from '../BrandLogo.vue'

describe('BrandLogo', () => {
  it('falls back to the shipped png when no site logo is configured', () => {
    const wrapper = mount(BrandLogo, { props: { alt: '雨柠' } })

    expect(wrapper.get('img').attributes('src')).toBe('/logo.png')
    expect(wrapper.get('img').attributes('alt')).toBe('雨柠')
  })

  it('keeps a portrait site logo uncropped', () => {
    const wrapper = mount(BrandLogo, {
      props: {
        src: '/characters/character-purple-cat.jpg',
        size: 'lg',
      },
    })

    expect(wrapper.classes()).toContain('brand-logo-lg')
    expect(wrapper.get('img').attributes('src')).toBe('/characters/character-purple-cat.jpg')
    expect(wrapper.get('img').classes()).toContain('brand-logo-img')
  })
})
