<template>
  <div class="global-character-display">
    <!-- 全屏角色背景（半透明） -->
    <div
      class="character-background"
      :style="{
        background: currentGradient
      }"
    ></div>

    <!-- 全屏角色展示 -->
    <div 
      class="character-showcase"
      @mouseenter="pauseRotation"
      @mouseleave="resumeRotation"
    >
      <!-- 发光背景 -->
      <div 
        class="glow-background"
        :style="{ boxShadow: currentGlow }"
      ></div>

      <!-- 角色图片（大尺寸） -->
      <div class="character-container">
        <transition name="character-fade" mode="out-in">
          <img
            :key="currentIndex"
            :src="currentCharacter.image"
            :alt="currentCharacter.name"
            class="character-image"
          />
        </transition>
      </div>

      <!-- 角色名称标签 -->
      <div class="character-badge">
        <div class="badge-content">
          <span class="character-name">{{ currentCharacter.name }}</span>
          <span class="character-subtitle">{{ currentCharacter.description }}</span>
        </div>
      </div>

    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useCharacterTheme } from '@/composables/useCharacterTheme'

interface Character {
  name: string
  description: string
  image: string
  themeId: string
}

const characters: Character[] = [
  {
    name: '紫夜',
    description: '神秘的魔法使',
    image: '/characters/character-purple-cat.jpg',
    themeId: 'cyber'
  },
  {
    name: '樱音',
    description: '温柔的治愈师',
    image: '/characters/character-pink.jpg',
    themeId: 'sweet'
  },
  {
    name: '冰璃',
    description: '冰雪公主',
    image: '/characters/character-ice-blue.jpg',
    themeId: 'fresh'
  },
  {
    name: '米瑟',
    description: '温暖守护者',
    image: '/characters/character-beige.jpg',
    themeId: 'amber'
  },
  {
    name: '樱舞',
    description: '春日精灵',
    image: '/characters/character-sakura-pink.jpg',
    themeId: 'sunset'
  },
  {
    name: '彩梦',
    description: '彩虹歌姬',
    image: '/characters/character-colorful-pink.jpg',
    themeId: 'starry'
  },
  {
    name: '花音',
    description: '花之使者',
    image: '/characters/character-flower.jpg',
    themeId: 'moonlight'
  },
  {
    name: '素心',
    description: '纯白天使',
    image: '/characters/character-sketch-1.jpg',
    themeId: 'nature'
  },
  {
    name: '雨绯',
    description: '霓虹雨夜的游侠',
    image: '/characters/character-rain-neon.jpg',
    themeId: 'cyber'
  },
  {
    name: '忆晨',
    description: '温暖的陪伴者',
    image: '/characters/character-amber-warm.jpg',
    themeId: 'amber'
  },
  {
    name: '泣樱',
    description: '温柔的守护者',
    image: '/characters/character-tearful-pink.jpg',
    themeId: 'sweet'
  },
  {
    name: '霜蓝',
    description: '冷静的技术顾问',
    image: '/characters/character-frost-blue.jpg',
    themeId: 'fresh'
  },
  {
    name: '糖果',
    description: '活泼的校园向导',
    image: '/characters/character-candy-twin.jpg',
    themeId: 'starry'
  },
  {
    name: '心笺',
    description: '手绘的留言助手',
    image: '/characters/character-sketch-heart.jpg',
    themeId: 'sweet'
  },
  {
    name: '蓝扇',
    description: '优雅的和风礼仪官',
    image: '/characters/character-kimono-fan.jpg',
    themeId: 'fresh'
  },
  {
    name: '慍眉',
    description: '直言的效率督察员',
    image: '/characters/character-braid-annoyed.jpg',
    themeId: 'nature'
  },
  {
    name: '风信',
    description: '自由的探索先锋',
    image: '/characters/character-windswept-sketch.jpg',
    themeId: 'sunset'
  },
  {
    name: '忧灰',
    description: '沉静的深度思考者',
    image: '/characters/character-melancholy-sketch.jpg',
    themeId: 'moonlight'
  },
  {
    name: '冷萱',
    description: '一丝不苟的规则守护者',
    image: '/characters/character-braid-serious.jpg',
    themeId: 'nature'
  }
]

const currentIndex = ref(0)
const isPaused = ref(false)
let rotationTimer: number | null = null

const { switchTheme, getTheme } = useCharacterTheme()

const currentCharacter = computed(() => characters[currentIndex.value])

const currentTheme = computed(() => getTheme(currentCharacter.value.themeId))

const currentGradient = computed(() => 
  currentTheme.value?.gradients.background || 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)'
)

const currentGlow = computed(() => 
  `0 0 80px ${currentTheme.value?.glow || 'rgba(102, 126, 234, 0.4)'},
   0 0 120px ${currentTheme.value?.glow || 'rgba(102, 126, 234, 0.3)'}`
)

function switchTo(index: number) {
  currentIndex.value = index
  switchTheme(characters[index].themeId)
  resetRotationTimer()
}

function nextCharacter() {
  const next = (currentIndex.value + 1) % characters.length
  switchTo(next)
}

function pauseRotation() {
  isPaused.value = true
}

function resumeRotation() {
  isPaused.value = false
  resetRotationTimer()
}

function startRotation() {
  rotationTimer = window.setInterval(() => {
    if (!isPaused.value) {
      nextCharacter()
    }
  }, 8000)
}

function resetRotationTimer() {
  if (rotationTimer) {
    clearInterval(rotationTimer)
  }
  startRotation()
}

onMounted(() => {
  // 初始化主题
  switchTheme(characters[0].themeId)
  // 开始自动轮换
  startRotation()
})

onUnmounted(() => {
  if (rotationTimer) {
    clearInterval(rotationTimer)
  }
})
</script>

<style scoped>
.global-character-display {
  position: fixed;
  inset: 0;
  pointer-events: none;
  z-index: 0;
}

/* Soft wash only — never a competing full-bleed wallpaper. */
.character-background {
  position: absolute;
  inset: 0;
  transition: all 1.2s cubic-bezier(0.4, 0, 0.2, 1);
  opacity: 0.08;
  pointer-events: none;
}

/* Pin the portrait to the right so controls stay readable. */
.character-showcase {
  position: fixed;
  top: 0;
  right: 0;
  bottom: 0;
  width: min(52vw, 720px);
  pointer-events: none;
  z-index: 1;
}

.glow-background {
  position: absolute;
  inset: 12% 8% 18% 18%;
  border-radius: 40%;
  opacity: 0.35;
  transition: box-shadow 1.2s cubic-bezier(0.4, 0, 0.2, 1);
  pointer-events: none;
}

.character-container {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: flex-end;
  justify-content: flex-end;
  overflow: hidden;
  mask-image: linear-gradient(90deg, transparent 0%, rgba(0, 0, 0, 0.45) 10%, #000 28%);
  -webkit-mask-image: linear-gradient(90deg, transparent 0%, rgba(0, 0, 0, 0.45) 10%, #000 28%);
}

.character-container::after {
  content: '';
  position: absolute;
  inset: 0;
  pointer-events: none;
  background: linear-gradient(
    90deg,
    var(--ds-bg, #f8fafc) 0%,
    color-mix(in srgb, var(--ds-bg, #f8fafc) 35%, transparent) 18%,
    transparent 48%
  );
}

:global(html.dark) .character-container::after {
  background: linear-gradient(
    90deg,
    var(--ds-bg, #020617) 0%,
    color-mix(in srgb, var(--ds-bg, #020617) 35%, transparent) 18%,
    transparent 48%
  );
}

.character-image {
  width: auto;
  height: 100%;
  max-width: none;
  object-fit: contain;
  object-position: right bottom;
  filter: drop-shadow(0 18px 40px rgba(15, 23, 42, 0.28));
  transition: transform 0.6s cubic-bezier(0.4, 0, 0.2, 1);
  opacity: 1;
}

.character-fade-enter-active,
.character-fade-leave-active {
  transition: opacity 0.6s cubic-bezier(0.4, 0, 0.2, 1), 
              transform 0.6s cubic-bezier(0.4, 0, 0.2, 1);
}

.character-fade-enter-from {
  opacity: 0;
  transform: translateX(60px) scale(0.95);
}

.character-fade-leave-to {
  opacity: 0;
  transform: translateX(-60px) scale(0.95);
}

.character-badge {
  position: absolute;
  right: 24px;
  bottom: 28px;
  left: auto;
  background: color-mix(in srgb, var(--ds-surface, #fff) 92%, transparent);
  backdrop-filter: blur(20px);
  border-radius: 16px;
  padding: 12px 16px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.12),
              0 2px 8px rgba(0, 0, 0, 0.08);
  border: 1px solid color-mix(in srgb, var(--ds-border, #e5e7eb) 80%, transparent);
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  pointer-events: auto;
  z-index: 10;
}

.character-showcase:hover .character-badge {
  transform: translateY(-4px);
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.16),
              0 4px 12px rgba(0, 0, 0, 0.12);
}

.badge-content {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.character-name {
  font-size: 20px;
  font-weight: 700;
  background: linear-gradient(135deg, 
    var(--color-primary-600), 
    var(--color-primary-500)
  );
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  letter-spacing: 0.5px;
}

.character-subtitle {
  font-size: 13px;
  color: var(--color-primary-400);
  font-weight: 500;
}

/* 响应式：小屏幕不显示角色背景，保证表单和内容空间 */
@media (max-width: 1024px) {
  .character-showcase {
    display: none;
  }
}

/* Dark mode 适配 */
.dark .character-badge {
  background: rgba(15, 23, 42, 0.95);
  border-color: rgba(255, 255, 255, 0.1);
}

.dark .character-name {
  -webkit-text-fill-color: transparent;
}

.dark .character-subtitle {
  color: var(--color-primary-300);
}
</style>
