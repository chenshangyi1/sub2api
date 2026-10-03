<template>
  <div class="character-display">
    <!-- 角色容器 -->
    <div 
      class="character-wrapper"
      @mouseenter="handleMouseEnter"
      @mouseleave="handleMouseLeave"
    >
      <!-- 背景光晕效果 -->
      <div 
        class="character-glow"
        :style="{ background: currentGlowColor }"
      ></div>
      
      <!-- 角色图片 -->
      <div class="character-image-container">
        <img
          :src="currentCharacter.image"
          :alt="currentCharacter.name"
          class="character-image"
          :class="{ 'hovered': isHovered }"
        />
      </div>

      <!-- 装饰元素 -->
      <div class="character-decorations">
        <!-- 顶部装饰线 -->
        <div class="decoration-line top"></div>
        <!-- 底部装饰线 -->
        <div class="decoration-line bottom"></div>
        
        <!-- 浮动粒子 -->
        <div class="particles">
          <div 
            v-for="i in 8" 
            :key="i" 
            class="particle"
            :style="{ 
              left: `${Math.random() * 100}%`,
              animationDelay: `${Math.random() * 3}s`,
              animationDuration: `${3 + Math.random() * 2}s`
            }"
          ></div>
        </div>
      </div>

      <!-- 角色信息卡片 -->
      <div class="character-info" :class="{ 'show': isHovered }">
        <div class="info-content">
          <h3 class="character-name">{{ currentCharacter.name }}</h3>
          <p class="character-description">{{ currentCharacter.description }}</p>
          <div class="character-tags">
            <span 
              v-for="tag in currentCharacter.tags" 
              :key="tag"
              class="tag"
            >
              {{ tag }}
            </span>
          </div>
        </div>
      </div>
    </div>

    <!-- 角色切换按钮 -->
    <div class="character-controls">
      <button
        v-for="(char, index) in characters"
        :key="index"
        @click="switchCharacter(index)"
        class="control-dot"
        :class="{ 'active': currentIndex === index }"
        :aria-label="`切换到${char.name}`"
      ></button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useCharacterTheme } from '@/composables/useCharacterTheme'

interface Character {
  name: string
  description: string
  image: string
  themeId: string // 关联的主题 ID
  tags: string[]
}

const { switchTheme } = useCharacterTheme()

const characters = ref<Character[]>([
  {
    name: '紫夜',
    description: '赛博空间的守护者，擅长处理复杂的模型调度任务',
    image: '/characters/character-purple-cat.jpg',
    themeId: 'cyber',
    tags: ['赛博', '高效', 'AI助手']
  },
  {
    name: '樱音',
    description: '温柔的模型管理助手，为你带来最佳的使用体验',
    image: '/characters/character-pink.jpg',
    themeId: 'sweet',
    tags: ['温柔', '贴心', '智能']
  },
  {
    name: '冰璃',
    description: '清爽专业的数据分析师，擅长将复杂信息可视化',
    image: '/characters/character-fresh.svg',
    themeId: 'fresh',
    tags: ['清爽', '专业', '精准']
  },
  {
    name: '翠岚',
    description: '温柔的帮助向导，引导你探索每个功能的奥秘',
    image: '/characters/character-nature.svg',
    themeId: 'nature',
    tags: ['温柔', '友好', '治愈']
  },
  {
    name: '焰舞',
    description: '热情洋溢的活动助手，为你带来激动人心的优惠',
    image: '/characters/character-sunset.svg',
    themeId: 'sunset',
    tags: ['热情', '活力', '激情']
  },
  {
    name: '星紫',
    description: '神秘优雅的高级顾问，为VIP用户提供专属服务',
    image: '/characters/character-starry.svg',
    themeId: 'starry',
    tags: ['神秘', '优雅', '高贵']
  },
  {
    name: '月银',
    description: '简约高效的企业管理员，专注于核心业务流程',
    image: '/characters/character-moonlight.svg',
    themeId: 'moonlight',
    tags: ['简约', '高效', '专业']
  },
  {
    name: '琥珀',
    description: '知性温柔的知识管理者，为你整理最有价值的信息',
    image: '/characters/character-amber.svg',
    themeId: 'amber',
    tags: ['知性', '温暖', '博学']
  }
])

const currentIndex = ref(0)
const isHovered = ref(false)
let autoSwitchInterval: number | null = null

const currentCharacter = computed(() => characters.value[currentIndex.value])

// 动态计算光晕颜色（从主题系统获取）
const currentGlowColor = computed(() => {
  const themeId = currentCharacter.value.themeId
  if (themeId === 'cyber') {
    return 'radial-gradient(circle, rgba(139, 92, 246, 0.4) 0%, transparent 70%)'
  } else if (themeId === 'sweet') {
    return 'radial-gradient(circle, rgba(244, 114, 182, 0.35) 0%, transparent 70%)'
  }
  return 'radial-gradient(circle, rgba(139, 92, 246, 0.4) 0%, transparent 70%)'
})

const switchCharacter = (index: number) => {
  currentIndex.value = index
  resetAutoSwitch()
}

// 监听角色切换，自动切换主题
watch(currentIndex, (newIndex) => {
  const character = characters.value[newIndex]
  switchTheme(character.themeId)
})

const handleMouseEnter = () => {
  isHovered.value = true
  // 鼠标悬停时暂停自动切换
  if (autoSwitchInterval) {
    clearInterval(autoSwitchInterval)
    autoSwitchInterval = null
  }
}

const handleMouseLeave = () => {
  isHovered.value = false
  // 鼠标离开后恢复自动切换
  startAutoSwitch()
}

const startAutoSwitch = () => {
  if (characters.value.length <= 1) return
  
  autoSwitchInterval = window.setInterval(() => {
    currentIndex.value = (currentIndex.value + 1) % characters.value.length
  }, 5000) // 每5秒切换一次
}

const resetAutoSwitch = () => {
  if (autoSwitchInterval) {
    clearInterval(autoSwitchInterval)
  }
  startAutoSwitch()
}

onMounted(() => {
  // 初始化主题
  switchTheme(currentCharacter.value.themeId)
  startAutoSwitch()
})

onUnmounted(() => {
  if (autoSwitchInterval) {
    clearInterval(autoSwitchInterval)
  }
})
</script>

<style scoped>
.character-display {
  position: relative;
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}

.character-wrapper {
  position: relative;
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  overflow: hidden;
}

/* 背景光晕 */
.character-glow {
  position: absolute;
  width: 120%;
  height: 120%;
  top: -10%;
  left: -10%;
  filter: blur(60px);
  opacity: 0.6;
  transition: opacity 0.6s ease, transform 0.6s ease;
  pointer-events: none;
  animation: glow-pulse 4s ease-in-out infinite;
}

@keyframes glow-pulse {
  0%, 100% {
    opacity: 0.4;
    transform: scale(1);
  }
  50% {
    opacity: 0.7;
    transform: scale(1.1);
  }
}

/* 角色图片容器 */
.character-image-container {
  position: relative;
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 2;
}

.character-image {
  max-width: 100%;
  max-height: 100%;
  object-fit: contain;
  filter: drop-shadow(0 10px 30px rgba(0, 0, 0, 0.3));
  transition: transform 0.6s cubic-bezier(0.34, 1.56, 0.64, 1), filter 0.3s ease;
  animation: character-entrance 1s ease-out;
}

@keyframes character-entrance {
  from {
    opacity: 0;
    transform: translateY(30px) scale(0.9);
  }
  to {
    opacity: 1;
    transform: translateY(0) scale(1);
  }
}

.character-image.hovered {
  transform: translateY(-8px) scale(1.02);
  filter: drop-shadow(0 15px 40px rgba(125, 111, 178, 0.4));
}

/* 装饰线条 */
.character-decorations {
  position: absolute;
  inset: 0;
  pointer-events: none;
  z-index: 1;
}

.decoration-line {
  position: absolute;
  left: 10%;
  right: 10%;
  height: 2px;
  background: linear-gradient(
    90deg,
    transparent,
    rgba(125, 111, 178, 0.4) 20%,
    rgba(125, 111, 178, 0.6) 50%,
    rgba(125, 111, 178, 0.4) 80%,
    transparent
  );
  opacity: 0.5;
}

.decoration-line.top {
  top: 10%;
  animation: line-slide-right 3s ease-in-out infinite;
}

.decoration-line.bottom {
  bottom: 10%;
  animation: line-slide-left 3s ease-in-out infinite;
}

@keyframes line-slide-right {
  0%, 100% {
    transform: translateX(0);
  }
  50% {
    transform: translateX(20px);
  }
}

@keyframes line-slide-left {
  0%, 100% {
    transform: translateX(0);
  }
  50% {
    transform: translateX(-20px);
  }
}

/* 浮动粒子 */
.particles {
  position: absolute;
  inset: 0;
  overflow: hidden;
}

.particle {
  position: absolute;
  width: 4px;
  height: 4px;
  background: radial-gradient(circle, rgba(184, 168, 216, 0.8), transparent);
  border-radius: 50%;
  animation: particle-float 4s ease-in-out infinite;
}

@keyframes particle-float {
  0% {
    transform: translateY(100%) scale(0);
    opacity: 0;
  }
  10% {
    opacity: 1;
  }
  90% {
    opacity: 1;
  }
  100% {
    transform: translateY(-100%) scale(1);
    opacity: 0;
  }
}

/* 角色信息卡片 */
.character-info {
  position: absolute;
  bottom: 8%;
  left: 50%;
  transform: translateX(-50%) translateY(20px);
  background: linear-gradient(135deg, rgba(255, 255, 255, 0.95) 0%, rgba(245, 243, 247, 0.9) 100%);
  backdrop-filter: blur(20px);
  border: 1px solid rgba(184, 168, 216, 0.3);
  border-radius: 16px;
  padding: 16px 24px;
  box-shadow: 0 8px 32px rgba(125, 111, 178, 0.2);
  opacity: 0;
  pointer-events: none;
  transition: all 0.4s cubic-bezier(0.34, 1.56, 0.64, 1);
  z-index: 10;
  min-width: 280px;
  max-width: 90%;
}

.dark .character-info {
  background: linear-gradient(135deg, rgba(42, 36, 63, 0.95) 0%, rgba(58, 51, 89, 0.9) 100%);
  border-color: rgba(125, 111, 178, 0.4);
}

.character-info.show {
  opacity: 1;
  transform: translateX(-50%) translateY(0);
  pointer-events: auto;
}

.info-content {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.character-name {
  font-size: 18px;
  font-weight: 600;
  background: linear-gradient(135deg, #7d6fb2 0%, #9b8cc5 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  margin: 0;
}

.character-description {
  font-size: 13px;
  color: #6b7280;
  margin: 0;
  line-height: 1.5;
}

.dark .character-description {
  color: #9ca3af;
}

.character-tags {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  margin-top: 4px;
}

.tag {
  display: inline-block;
  font-size: 11px;
  padding: 3px 10px;
  background: linear-gradient(135deg, rgba(125, 111, 178, 0.15) 0%, rgba(155, 140, 197, 0.1) 100%);
  border: 1px solid rgba(125, 111, 178, 0.25);
  border-radius: 12px;
  color: #7d6fb2;
  font-weight: 500;
}

.dark .tag {
  background: linear-gradient(135deg, rgba(125, 111, 178, 0.2) 0%, rgba(155, 140, 197, 0.15) 100%);
  border-color: rgba(125, 111, 178, 0.3);
  color: #b8a8d8;
}

/* 角色切换控制 */
.character-controls {
  position: absolute;
  bottom: 20px;
  left: 50%;
  transform: translateX(-50%);
  display: flex;
  gap: 10px;
  z-index: 20;
  padding: 8px 16px;
  background: rgba(255, 255, 255, 0.8);
  backdrop-filter: blur(10px);
  border-radius: 20px;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
}

.dark .character-controls {
  background: rgba(42, 36, 63, 0.8);
}

.control-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #d1c7db;
  border: none;
  cursor: pointer;
  transition: all 0.3s ease;
  padding: 0;
}

.control-dot:hover {
  background: #b8a8d8;
  transform: scale(1.2);
}

.control-dot.active {
  width: 24px;
  border-radius: 4px;
  background: linear-gradient(135deg, #7d6fb2 0%, #9b8cc5 100%);
}

/* 响应式 */
@media (max-width: 768px) {
  .character-info {
    min-width: 240px;
    padding: 12px 16px;
  }

  .character-name {
    font-size: 16px;
  }

  .character-description {
    font-size: 12px;
  }

  .decoration-line {
    left: 5%;
    right: 5%;
  }
}
</style>
