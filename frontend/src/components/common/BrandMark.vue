<template>
  <span class="brand-mark" :class="[`brand-mark-${size}`, { 'brand-mark-custom': isCustomLogo }]">
    <img
      :src="resolvedLogo"
      :alt="decorative ? '' : alt"
      :aria-hidden="decorative ? 'true' : undefined"
      class="brand-mark-image"
    />
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { defaultBrandLogo, resolveBrandLogo } from '@/utils/branding'

const props = withDefaults(defineProps<{
  src?: string
  alt?: string
  decorative?: boolean
  size?: 'sm' | 'md' | 'lg'
}>(), {
  src: '',
  alt: 'Sub2API',
  decorative: false,
  size: 'md',
})

const resolvedLogo = computed(() => resolveBrandLogo(props.src))
const isCustomLogo = computed(() => resolvedLogo.value !== defaultBrandLogo)
</script>

<style scoped>
.brand-mark {
  display: inline-flex;
  flex: none;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  border: 1px solid color-mix(in srgb, var(--ds-border-strong) 72%, transparent);
  border-radius: 8px;
  background: #fff;
  box-shadow: 0 1px 2px rgba(28, 25, 23, 0.08);
}

.brand-mark-sm {
  width: 2.25rem;
  height: 2.25rem;
}

.brand-mark-md {
  width: 2.5rem;
  height: 2.5rem;
}

.brand-mark-lg {
  width: 3rem;
  height: 3rem;
}

.brand-mark-image {
  width: 100%;
  height: 100%;
  object-fit: cover;
  object-position: center 22%;
}

.brand-mark-custom .brand-mark-image {
  object-fit: contain;
  object-position: center;
}

:global(html.dark) .brand-mark {
  border-color: var(--ds-border);
  background: var(--ds-surface);
}
</style>
