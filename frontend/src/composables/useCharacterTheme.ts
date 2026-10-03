import { ref, computed, watch } from 'vue'

export interface CharacterTheme {
  id: string
  name: string
  // 主色调
  primary: {
    50: string
    100: string
    200: string
    300: string
    400: string
    500: string
    600: string
    700: string
    800: string
    900: string
    950: string
  }
  // 强调色
  accent: {
    50: string
    100: string
    200: string
    300: string
    400: string
    500: string
    600: string
    700: string
    800: string
    900: string
    950: string
  }
  // 渐变配置
  gradients: {
    background: string
    card: string
    button: string
    text: string
  }
  // 光晕效果
  glow: string
}

// 角色主题配置
export const characterThemes: Record<string, CharacterTheme> = {
  // 紫色猫耳少女 - 赛博朋克主题
  cyber: {
    id: 'cyber',
    name: '赛博紫蓝',
    primary: {
      50: '#f5f3ff',
      100: '#ede9fe',
      200: '#ddd6fe',
      300: '#c4b5fd',
      400: '#a78bfa',
      500: '#8b5cf6', // 主色
      600: '#7c3aed',
      700: '#6d28d9',
      800: '#5b21b6',
      900: '#4c1d95',
      950: '#2e1065'
    },
    accent: {
      50: '#fdf4ff',
      100: '#fae8ff',
      200: '#f5d0fe',
      300: '#f0abfc',
      400: '#e879f9',
      500: '#ec4899', // 强调色 - 电光粉
      600: '#db2777',
      700: '#be185d',
      800: '#9f1239',
      900: '#831843',
      950: '#500724'
    },
    gradients: {
      background: `
        radial-gradient(circle at 8% -10%, rgba(139, 92, 246, 0.15), transparent 34rem),
        radial-gradient(circle at 92% 0%, rgba(236, 72, 153, 0.12), transparent 30rem),
        linear-gradient(180deg, #0f0f1e 0%, #1a1625 52%, #0f0f1e 100%)
      `,
      card: 'linear-gradient(135deg, rgba(139, 92, 246, 0.05) 0%, rgba(236, 72, 153, 0.03) 100%)',
      button: 'linear-gradient(135deg, #8b5cf6 0%, #7c3aed 50%, #ec4899 100%)',
      text: 'linear-gradient(135deg, #8b5cf6 0%, #c4b5fd 100%)'
    },
    glow: 'radial-gradient(circle, rgba(139, 92, 246, 0.4) 0%, transparent 70%)'
  },

  // 樱音 - 温柔甜美主题
  sweet: {
    id: 'sweet',
    name: '樱花粉橙',
    primary: {
      50: '#fdf2f8',
      100: '#fce7f3',
      200: '#fbcfe8',
      300: '#f9a8d4',
      400: '#f472b6',
      500: '#ec4899', // 主色 - 樱花粉
      600: '#db2777',
      700: '#be185d',
      800: '#9f1239',
      900: '#831843',
      950: '#500724'
    },
    accent: {
      50: '#fff7ed',
      100: '#ffedd5',
      200: '#fed7aa',
      300: '#fdba74',
      400: '#fb923c', // 强调色 - 蜜桃橙
      500: '#f97316',
      600: '#ea580c',
      700: '#c2410c',
      800: '#9a3412',
      900: '#7c2d12',
      950: '#431407'
    },
    gradients: {
      background: `
        radial-gradient(circle at 8% -10%, rgba(244, 114, 182, 0.12), transparent 34rem),
        radial-gradient(circle at 92% 0%, rgba(251, 146, 60, 0.1), transparent 30rem),
        linear-gradient(180deg, #fff5f7 0%, #ffe4e9 52%, #fff5f7 100%)
      `,
      card: 'linear-gradient(135deg, rgba(244, 114, 182, 0.08) 0%, rgba(251, 146, 60, 0.05) 100%)',
      button: 'linear-gradient(135deg, #ec4899 0%, #f472b6 50%, #fb923c 100%)',
      text: 'linear-gradient(135deg, #ec4899 0%, #f9a8d4 100%)'
    },
    glow: 'radial-gradient(circle, rgba(244, 114, 182, 0.35) 0%, transparent 70%)'
  },

  // 冰璃 - 天空蓝主题
  fresh: {
    id: 'fresh',
    name: '天空蓝',
    primary: {
      50: '#f0f9ff',
      100: '#e0f2fe',
      200: '#bae6fd',
      300: '#7dd3fc',
      400: '#38bdf8',
      500: '#0ea5e9',
      600: '#0284c7',
      700: '#0369a1',
      800: '#075985',
      900: '#0c4a6e',
      950: '#082f49'
    },
    accent: {
      50: '#f0fdfa',
      100: '#ccfbf1',
      200: '#99f6e4',
      300: '#5eead4',
      400: '#2dd4bf',
      500: '#14b8a6',
      600: '#0d9488',
      700: '#0f766e',
      800: '#115e59',
      900: '#134e4a',
      950: '#042f2e'
    },
    gradients: {
      background: `
        radial-gradient(circle at 8% -10%, rgba(14, 165, 233, 0.15), transparent 34rem),
        radial-gradient(circle at 92% 0%, rgba(20, 184, 166, 0.12), transparent 30rem),
        linear-gradient(180deg, #f0f9ff 0%, #e0f2fe 52%, #f0f9ff 100%)
      `,
      card: 'linear-gradient(135deg, rgba(14, 165, 233, 0.06) 0%, rgba(20, 184, 166, 0.04) 100%)',
      button: 'linear-gradient(135deg, #0ea5e9 0%, #0284c7 50%, #14b8a6 100%)',
      text: 'linear-gradient(135deg, #0ea5e9 0%, #38bdf8 100%)'
    },
    glow: 'radial-gradient(circle, rgba(14, 165, 233, 0.3) 0%, transparent 70%)'
  },

  // 翠岚 - 翡翠绿主题
  nature: {
    id: 'nature',
    name: '翡翠绿',
    primary: {
      50: '#f0fdf4',
      100: '#dcfce7',
      200: '#bbf7d0',
      300: '#86efac',
      400: '#4ade80',
      500: '#10b981',
      600: '#059669',
      700: '#047857',
      800: '#065f46',
      900: '#064e3b',
      950: '#022c22'
    },
    accent: {
      50: '#f7fee7',
      100: '#ecfccb',
      200: '#d9f99d',
      300: '#bef264',
      400: '#a3e635',
      500: '#84cc16',
      600: '#65a30d',
      700: '#4d7c0f',
      800: '#3f6212',
      900: '#365314',
      950: '#1a2e05'
    },
    gradients: {
      background: `
        radial-gradient(circle at 8% -10%, rgba(16, 185, 129, 0.12), transparent 34rem),
        radial-gradient(circle at 92% 0%, rgba(132, 204, 22, 0.1), transparent 30rem),
        linear-gradient(180deg, #f0fdf4 0%, #dcfce7 52%, #f0fdf4 100%)
      `,
      card: 'linear-gradient(135deg, rgba(16, 185, 129, 0.06) 0%, rgba(132, 204, 22, 0.04) 100%)',
      button: 'linear-gradient(135deg, #10b981 0%, #059669 50%, #84cc16 100%)',
      text: 'linear-gradient(135deg, #10b981 0%, #4ade80 100%)'
    },
    glow: 'radial-gradient(circle, rgba(16, 185, 129, 0.3) 0%, transparent 70%)'
  },

  // 焰舞 - 日落橙红主题
  sunset: {
    id: 'sunset',
    name: '日落橙红',
    primary: {
      50: '#fff7ed',
      100: '#ffedd5',
      200: '#fed7aa',
      300: '#fdba74',
      400: '#fb923c',
      500: '#f97316',
      600: '#ea580c',
      700: '#c2410c',
      800: '#9a3412',
      900: '#7c2d12',
      950: '#431407'
    },
    accent: {
      50: '#fef2f2',
      100: '#fee2e2',
      200: '#fecaca',
      300: '#fca5a5',
      400: '#f87171',
      500: '#ef4444',
      600: '#dc2626',
      700: '#b91c1c',
      800: '#991b1b',
      900: '#7f1d1d',
      950: '#450a0a'
    },
    gradients: {
      background: `
        radial-gradient(circle at 8% -10%, rgba(249, 115, 22, 0.12), transparent 34rem),
        radial-gradient(circle at 92% 0%, rgba(239, 68, 68, 0.1), transparent 30rem),
        linear-gradient(180deg, #fff7ed 0%, #ffedd5 52%, #fff7ed 100%)
      `,
      card: 'linear-gradient(135deg, rgba(249, 115, 22, 0.06) 0%, rgba(239, 68, 68, 0.04) 100%)',
      button: 'linear-gradient(135deg, #f97316 0%, #ea580c 50%, #ef4444 100%)',
      text: 'linear-gradient(135deg, #f97316 0%, #fb923c 100%)'
    },
    glow: 'radial-gradient(circle, rgba(249, 115, 22, 0.35) 0%, transparent 70%)'
  },

  // 星紫 - 星空紫主题
  starry: {
    id: 'starry',
    name: '星空紫',
    primary: {
      50: '#faf5ff',
      100: '#f3e8ff',
      200: '#e9d5ff',
      300: '#d8b4fe',
      400: '#c084fc',
      500: '#a855f7',
      600: '#9333ea',
      700: '#7e22ce',
      800: '#6b21a8',
      900: '#581c87',
      950: '#3b0764'
    },
    accent: {
      50: '#eef2ff',
      100: '#e0e7ff',
      200: '#c7d2fe',
      300: '#a5b4fc',
      400: '#818cf8',
      500: '#6366f1',
      600: '#4f46e5',
      700: '#4338ca',
      800: '#3730a3',
      900: '#312e81',
      950: '#1e1b4b'
    },
    gradients: {
      background: `
        radial-gradient(circle at 8% -10%, rgba(168, 85, 247, 0.12), transparent 34rem),
        radial-gradient(circle at 92% 0%, rgba(99, 102, 241, 0.1), transparent 30rem),
        linear-gradient(180deg, #1a0a2e 0%, #2d1b4e 52%, #1a0a2e 100%)
      `,
      card: 'linear-gradient(135deg, rgba(168, 85, 247, 0.08) 0%, rgba(99, 102, 241, 0.05) 100%)',
      button: 'linear-gradient(135deg, #a855f7 0%, #9333ea 50%, #6366f1 100%)',
      text: 'linear-gradient(135deg, #a855f7 0%, #c084fc 100%)'
    },
    glow: 'radial-gradient(circle, rgba(168, 85, 247, 0.4) 0%, transparent 70%)'
  },

  // 月银 - 月光银灰主题
  moonlight: {
    id: 'moonlight',
    name: '月光银灰',
    primary: {
      50: '#f8fafc',
      100: '#f1f5f9',
      200: '#e2e8f0',
      300: '#cbd5e1',
      400: '#94a3b8',
      500: '#64748b',
      600: '#475569',
      700: '#334155',
      800: '#1e293b',
      900: '#0f172a',
      950: '#020617'
    },
    accent: {
      50: '#f8fafc',
      100: '#f1f5f9',
      200: '#e2e8f0',
      300: '#cbd5e1',
      400: '#94a3b8',
      500: '#64748b',
      600: '#475569',
      700: '#334155',
      800: '#1e293b',
      900: '#0f172a',
      950: '#020617'
    },
    gradients: {
      background: `
        radial-gradient(circle at 8% -10%, rgba(100, 116, 139, 0.08), transparent 34rem),
        radial-gradient(circle at 92% 0%, rgba(148, 163, 184, 0.06), transparent 30rem),
        linear-gradient(180deg, #f8fafc 0%, #f1f5f9 52%, #f8fafc 100%)
      `,
      card: 'linear-gradient(135deg, rgba(100, 116, 139, 0.04) 0%, rgba(148, 163, 184, 0.03) 100%)',
      button: 'linear-gradient(135deg, #64748b 0%, #475569 50%, #94a3b8 100%)',
      text: 'linear-gradient(135deg, #64748b 0%, #94a3b8 100%)'
    },
    glow: 'radial-gradient(circle, rgba(100, 116, 139, 0.2) 0%, transparent 70%)'
  },

  // 琥珀 - 金秋黄主题
  amber: {
    id: 'amber',
    name: '金秋黄',
    primary: {
      50: '#fffbeb',
      100: '#fef3c7',
      200: '#fde68a',
      300: '#fcd34d',
      400: '#fbbf24',
      500: '#f59e0b',
      600: '#d97706',
      700: '#b45309',
      800: '#92400e',
      900: '#78350f',
      950: '#451a03'
    },
    accent: {
      50: '#fefce8',
      100: '#fef9c3',
      200: '#fef08a',
      300: '#fde047',
      400: '#facc15',
      500: '#eab308',
      600: '#ca8a04',
      700: '#a16207',
      800: '#854d0e',
      900: '#713f12',
      950: '#422006'
    },
    gradients: {
      background: `
        radial-gradient(circle at 8% -10%, rgba(245, 158, 11, 0.12), transparent 34rem),
        radial-gradient(circle at 92% 0%, rgba(234, 179, 8, 0.1), transparent 30rem),
        linear-gradient(180deg, #fffbeb 0%, #fef3c7 52%, #fffbeb 100%)
      `,
      card: 'linear-gradient(135deg, rgba(245, 158, 11, 0.06) 0%, rgba(234, 179, 8, 0.04) 100%)',
      button: 'linear-gradient(135deg, #f59e0b 0%, #d97706 50%, #eab308 100%)',
      text: 'linear-gradient(135deg, #f59e0b 0%, #fbbf24 100%)'
    },
    glow: 'radial-gradient(circle, rgba(245, 158, 11, 0.3) 0%, transparent 70%)'
  }
}

// 当前激活的主题
const currentThemeId = ref<string>('cyber')

// 当前主题对象
export const currentTheme = computed(() => characterThemes[currentThemeId.value])

// 切换主题
export function switchTheme(themeId: string) {
  if (characterThemes[themeId]) {
    currentThemeId.value = themeId
    applyTheme(characterThemes[themeId])
  }
}

// 应用主题到 DOM
function applyTheme(theme: CharacterTheme) {
  const root = document.documentElement

  // 应用 CSS 变量
  Object.entries(theme.primary).forEach(([key, value]) => {
    root.style.setProperty(`--color-primary-${key}`, value)
  })

  Object.entries(theme.accent).forEach(([key, value]) => {
    root.style.setProperty(`--color-accent-${key}`, value)
  })

  // 应用渐变
  root.style.setProperty('--gradient-background', theme.gradients.background)
  root.style.setProperty('--gradient-card', theme.gradients.card)
  root.style.setProperty('--gradient-button', theme.gradients.button)
  root.style.setProperty('--gradient-text', theme.gradients.text)
  root.style.setProperty('--glow-color', theme.glow)
}

// 获取主题 ID 列表
export function getThemeIds(): string[] {
  return Object.keys(characterThemes)
}

// 获取主题名称
export function getThemeName(themeId: string): string {
  return characterThemes[themeId]?.name || themeId
}

// 初始化主题（在应用启动时调用）
export function initTheme() {
  applyTheme(currentTheme.value)
}

// 监听主题变化
watch(currentTheme, (newTheme) => {
  applyTheme(newTheme)
})

export function useCharacterTheme() {
  // 添加 getTheme 方法
  const getTheme = (themeId: string): CharacterTheme | undefined => {
    return characterThemes[themeId]
  }

  return {
    currentTheme,
    currentThemeId,
    switchTheme,
    getThemeIds,
    getThemeName,
    getTheme,
    initTheme
  }
}
