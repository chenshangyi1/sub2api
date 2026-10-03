<template>
  <div class="plaza-hero-section">
    <!-- 左侧：角色展示区 -->
    <div class="hero-character">
      <CharacterDisplay />
    </div>

    <!-- 右侧：欢迎信息和快速操作 -->
    <div class="hero-content">
      <!-- 主标题 -->
      <div class="hero-header">
        <h1 class="hero-title">
          <span class="title-gradient">模型广场</span>
        </h1>
        <p class="hero-subtitle">
          探索丰富的 AI 模型，找到最适合你的智能助手
        </p>
      </div>

      <!-- 特色标签 -->
      <div class="hero-features">
        <div class="feature-item">
          <div class="feature-icon">
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 10V3L4 14h7v7l9-11h-7z" />
            </svg>
          </div>
          <div class="feature-text">
            <span class="feature-label">高速响应</span>
            <span class="feature-value">< 100ms</span>
          </div>
        </div>

        <div class="feature-item">
          <div class="feature-icon">
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" />
            </svg>
          </div>
          <div class="feature-text">
            <span class="feature-label">稳定可靠</span>
            <span class="feature-value">99.9%</span>
          </div>
        </div>

        <div class="feature-item">
          <div class="feature-icon">
            <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3 .895 3 2-1.343 2-3 2m0-8c1.11 0 2.08.402 2.599 1M12 8V7m0 1v8m0 0v1m0-1c-1.11 0-2.08-.402-2.599-1M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          </div>
          <div class="feature-text">
            <span class="feature-label">灵活计费</span>
            <span class="feature-value">按需付费</span>
          </div>
        </div>
      </div>

      <!-- 统计数据 -->
      <div class="hero-stats">
        <div class="stat-item">
          <div class="stat-value">50+</div>
          <div class="stat-label">可用模型</div>
        </div>
        <div class="stat-divider"></div>
        <div class="stat-item">
          <div class="stat-value">10K+</div>
          <div class="stat-label">活跃用户</div>
        </div>
        <div class="stat-divider"></div>
        <div class="stat-item">
          <div class="stat-value">1M+</div>
          <div class="stat-label">日均请求</div>
        </div>
      </div>

      <!-- CTA 按钮 -->
      <div class="hero-actions">
        <button 
          @click="scrollToModels"
          class="btn-primary-gradient"
        >
          <span>浏览模型</span>
          <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
          </svg>
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import CharacterDisplay from './CharacterDisplay.vue'

const scrollToModels = () => {
  const modelsSection = document.querySelector('.space-y-6')
  if (modelsSection) {
    modelsSection.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }
}
</script>

<style scoped>
.plaza-hero-section {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 48px;
  margin-bottom: 48px;
  padding: 32px;
  background: linear-gradient(
    135deg,
    rgba(245, 243, 247, 0.6) 0%,
    rgba(255, 255, 255, 0.4) 50%,
    rgba(232, 227, 237, 0.6) 100%
  );
  border: 1px solid rgba(184, 168, 216, 0.2);
  border-radius: 24px;
  box-shadow: 0 8px 32px rgba(125, 111, 178, 0.1);
  backdrop-filter: blur(10px);
  position: relative;
  overflow: hidden;
}

.dark .plaza-hero-section {
  background: linear-gradient(
    135deg,
    rgba(42, 36, 63, 0.6) 0%,
    rgba(26, 29, 38, 0.4) 50%,
    rgba(58, 51, 89, 0.6) 100%
  );
  border-color: rgba(125, 111, 178, 0.3);
}

/* 背景装饰 */
.plaza-hero-section::before {
  content: '';
  position: absolute;
  top: -50%;
  right: -20%;
  width: 600px;
  height: 600px;
  background: radial-gradient(circle, rgba(184, 168, 216, 0.15) 0%, transparent 70%);
  border-radius: 50%;
  pointer-events: none;
  animation: float-decoration 20s ease-in-out infinite;
}

@keyframes float-decoration {
  0%, 100% {
    transform: translate(0, 0) rotate(0deg);
  }
  50% {
    transform: translate(-30px, 30px) rotate(180deg);
  }
}

/* 角色展示区 */
.hero-character {
  position: relative;
  height: 500px;
  display: flex;
  align-items: center;
  justify-content: center;
}

/* 内容区 */
.hero-content {
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 32px;
  padding: 24px 0;
}

/* 标题区 */
.hero-header {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.hero-title {
  font-size: 48px;
  font-weight: 700;
  line-height: 1.1;
  margin: 0;
}

.title-gradient {
  background: linear-gradient(135deg, #2d3748 0%, #7d6fb2 50%, #b8a8d8 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  animation: gradient-shift 3s ease infinite;
  background-size: 200% 200%;
}

@keyframes gradient-shift {
  0%, 100% {
    background-position: 0% 50%;
  }
  50% {
    background-position: 100% 50%;
  }
}

.hero-subtitle {
  font-size: 16px;
  color: #6b7280;
  margin: 0;
  line-height: 1.6;
}

.dark .hero-subtitle {
  color: #9ca3af;
}

/* 特色标签 */
.hero-features {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.feature-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  background: rgba(255, 255, 255, 0.6);
  border: 1px solid rgba(184, 168, 216, 0.2);
  border-radius: 12px;
  transition: all 0.3s ease;
}

.dark .feature-item {
  background: rgba(42, 36, 63, 0.4);
  border-color: rgba(125, 111, 178, 0.25);
}

.feature-item:hover {
  background: rgba(255, 255, 255, 0.9);
  border-color: rgba(125, 111, 178, 0.4);
  transform: translateX(4px);
}

.dark .feature-item:hover {
  background: rgba(42, 36, 63, 0.7);
  border-color: rgba(125, 111, 178, 0.4);
}

.feature-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  background: linear-gradient(135deg, #7d6fb2 0%, #9b8cc5 100%);
  border-radius: 10px;
  color: white;
  flex-shrink: 0;
}

.feature-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.feature-label {
  font-size: 13px;
  color: #6b7280;
  font-weight: 500;
}

.dark .feature-label {
  color: #9ca3af;
}

.feature-value {
  font-size: 15px;
  font-weight: 600;
  color: #7d6fb2;
}

.dark .feature-value {
  color: #b8a8d8;
}

/* 统计数据 */
.hero-stats {
  display: flex;
  align-items: center;
  gap: 24px;
  padding: 20px 24px;
  background: rgba(255, 255, 255, 0.8);
  border: 1px solid rgba(184, 168, 216, 0.25);
  border-radius: 16px;
  backdrop-filter: blur(10px);
}

.dark .hero-stats {
  background: rgba(42, 36, 63, 0.6);
  border-color: rgba(125, 111, 178, 0.3);
}

.stat-item {
  flex: 1;
  text-align: center;
}

.stat-value {
  font-size: 28px;
  font-weight: 700;
  background: linear-gradient(135deg, #7d6fb2 0%, #9b8cc5 100%);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  line-height: 1.2;
}

.stat-label {
  font-size: 12px;
  color: #6b7280;
  margin-top: 4px;
  font-weight: 500;
}

.dark .stat-label {
  color: #9ca3af;
}

.stat-divider {
  width: 1px;
  height: 40px;
  background: linear-gradient(
    to bottom,
    transparent,
    rgba(184, 168, 216, 0.4) 50%,
    transparent
  );
}

/* 按钮 */
.hero-actions {
  display: flex;
  gap: 12px;
}

.btn-primary-gradient {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 14px 32px;
  background: linear-gradient(135deg, #7d6fb2 0%, #9b8cc5 50%, #b8a8d8 100%);
  background-size: 200% 200%;
  color: white;
  font-size: 15px;
  font-weight: 600;
  border: none;
  border-radius: 12px;
  box-shadow: 0 8px 24px rgba(125, 111, 178, 0.3);
  cursor: pointer;
  transition: all 0.3s ease;
  animation: gradient-shift 3s ease infinite;
}

.btn-primary-gradient:hover {
  transform: translateY(-2px);
  box-shadow: 0 12px 32px rgba(125, 111, 178, 0.4);
}

.btn-primary-gradient:active {
  transform: translateY(0);
}

/* 响应式 */
@media (max-width: 1024px) {
  .plaza-hero-section {
    grid-template-columns: 1fr;
    gap: 32px;
    padding: 24px;
  }

  .hero-character {
    height: 400px;
    order: -1;
  }

  .hero-title {
    font-size: 36px;
  }

  .hero-stats {
    gap: 16px;
    padding: 16px 20px;
  }

  .stat-value {
    font-size: 24px;
  }
}

@media (max-width: 640px) {
  .plaza-hero-section {
    padding: 20px;
    gap: 24px;
  }

  .hero-character {
    height: 320px;
  }

  .hero-title {
    font-size: 28px;
  }

  .hero-subtitle {
    font-size: 14px;
  }

  .hero-stats {
    flex-direction: column;
    gap: 16px;
    padding: 20px;
  }

  .stat-divider {
    width: 80%;
    height: 1px;
  }

  .stat-item {
    width: 100%;
  }

  .hero-features {
    gap: 8px;
  }

  .feature-item {
    padding: 10px 12px;
  }
}
</style>
