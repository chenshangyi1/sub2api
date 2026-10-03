/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // 主色调 - 薰衣草紫/雅致灰紫（基于角色眼睛和发饰配色）
        primary: {
          50: '#f8f7fc',
          100: '#f0eef8',
          200: '#e8e3f5',
          300: '#d4c9ed',
          400: '#b8a8d8',
          500: '#9d8bc4',
          600: '#8570af',
          700: '#6d5a94',
          800: '#584878',
          900: '#463a5f',
          950: '#2e2540'
        },
        // 辅助色 - 冷灰蓝（基于角色头发和西装色调）
        accent: {
          50: '#f7f8fa',
          100: '#eef0f4',
          200: '#dde1e8',
          300: '#c4cad6',
          400: '#9ea7b8',
          500: '#7a8599',
          600: '#5f6a7d',
          700: '#4d5667',
          800: '#3f4451',
          900: '#2d323d',
          950: '#1a1d26'
        },
        // 深色模式背景（深邃优雅的石板灰）
        dark: {
          50: '#f7f8fa',
          100: '#eef0f4',
          200: '#dde1e8',
          300: '#c4cad6',
          400: '#9ea7b8',
          500: '#7a8599',
          600: '#5f6a7d',
          700: '#4d5667',
          800: '#3f4451',
          900: '#2d323d',
          950: '#1a1d26'
        }
      },
      fontFamily: {
        sans: [
          'system-ui',
          '-apple-system',
          'BlinkMacSystemFont',
          'Segoe UI',
          'Roboto',
          'Helvetica Neue',
          'Arial',
          'PingFang SC',
          'Hiragino Sans GB',
          'Microsoft YaHei',
          'sans-serif'
        ],
        mono: ['ui-monospace', 'SFMono-Regular', 'Menlo', 'Monaco', 'Consolas', 'monospace']
      },
      boxShadow: {
        glass: '0 8px 32px rgba(0, 0, 0, 0.08)',
        'glass-sm': '0 4px 16px rgba(0, 0, 0, 0.06)',
        glow: '0 0 20px rgba(157, 139, 196, 0.25)',
        'glow-lg': '0 0 40px rgba(133, 112, 175, 0.32)',
        card: '0 1px 3px rgba(0, 0, 0, 0.04), 0 1px 2px rgba(0, 0, 0, 0.06)',
        'card-hover': '0 10px 40px rgba(157, 139, 196, 0.12)',
        'inner-glow': 'inset 0 1px 0 rgba(255, 255, 255, 0.1)'
      },
      backgroundImage: {
        'gradient-radial': 'radial-gradient(var(--tw-gradient-stops))',
        'gradient-primary': 'linear-gradient(135deg, #b8a8d8 0%, #9d8bc4 55%, #8570af 100%)',
        'gradient-dark': 'linear-gradient(135deg, #3f4451 0%, #2d323d 100%)',
        'gradient-glass':
          'linear-gradient(135deg, rgba(255,255,255,0.1) 0%, rgba(255,255,255,0.05) 100%)',
        'mesh-gradient':
          'radial-gradient(at 40% 20%, rgba(184, 168, 216, 0.16) 0px, transparent 50%), radial-gradient(at 80% 0%, rgba(157, 139, 196, 0.10) 0px, transparent 50%), radial-gradient(at 0% 50%, rgba(133, 112, 175, 0.08) 0px, transparent 50%)'
      },
      animation: {
        'fade-in': 'fadeIn 0.3s ease-out',
        'slide-up': 'slideUp 0.3s ease-out',
        'slide-down': 'slideDown 0.3s ease-out',
        'slide-in-right': 'slideInRight 0.3s ease-out',
        'scale-in': 'scaleIn 0.2s ease-out',
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        shimmer: 'shimmer 2s linear infinite',
        glow: 'glow 2s ease-in-out infinite alternate'
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' }
        },
        slideUp: {
          '0%': { opacity: '0', transform: 'translateY(10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideDown: {
          '0%': { opacity: '0', transform: 'translateY(-10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideInRight: {
          '0%': { opacity: '0', transform: 'translateX(20px)' },
          '100%': { opacity: '1', transform: 'translateX(0)' }
        },
        scaleIn: {
          '0%': { opacity: '0', transform: 'scale(0.95)' },
          '100%': { opacity: '1', transform: 'scale(1)' }
        },
        shimmer: {
          '0%': { backgroundPosition: '-200% 0' },
          '100%': { backgroundPosition: '200% 0' }
        },
        glow: {
          '0%': { boxShadow: '0 0 20px rgba(157, 139, 196, 0.22)' },
          '100%': { boxShadow: '0 0 30px rgba(133, 112, 175, 0.35)' }
        }
      },
      backdropBlur: {
        xs: '2px'
      },
      borderRadius: {
        '4xl': '2rem'
      }
    }
  },
  plugins: []
}
