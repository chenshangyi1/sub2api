<template>
  <span
    class="brand-logo"
    :class="[`brand-logo-${size}`, { 'brand-logo-framed': framed }]"
    data-testid="brand-logo"
  >
    <img :src="src" :alt="alt" class="brand-logo-img" />
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { resolveBrandLogo } from '@/utils/branding'

const props = withDefaults(defineProps<{
  src?: string
  alt?: string
  size?: 'sm' | 'md' | 'lg'
  framed?: boolean
}>(), {
  src: '',
  alt: 'Logo',
  size: 'sm',
  framed: false,
})

const src = computed(() => resolveBrandLogo(props.src))
</script>

<style scoped>
.brand-logo {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  overflow: visible;
  background: transparent;
}

.brand-logo-img {
  display: block;
  width: auto;
  height: 100%;
  max-width: 100%;
  object-fit: contain;
  object-position: center top;
}

.brand-logo-sm {
  height: 2.5rem;
  max-width: 4.5rem;
}

.brand-logo-md {
  height: 4.5rem;
  max-width: 8rem;
}

.brand-logo-lg {
  height: 9rem;
  max-width: 14rem;
}

.brand-logo-framed {
  padding: 0.15rem;
}
</style>
