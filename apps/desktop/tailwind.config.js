/** @type {import('tailwindcss').Config} */
module.exports = {
  darkMode: ['class'],
  content: ['./src/renderer/**/*.{ts,tsx}', './src/renderer/index.html'],
  theme: {
    container: {
      center: true,
      padding: '2rem',
      screens: {
        '2xl': '1400px',
      },
    },
    extend: {
      colors: {
        border: 'hsl(var(--border))',
        input: 'hsl(var(--input))',
        ring: 'hsl(var(--ring))',
        background: 'hsl(var(--background))',
        foreground: 'hsl(var(--foreground))',
        primary: {
          DEFAULT: 'hsl(var(--primary))',
          foreground: 'hsl(var(--primary-foreground))',
        },
        secondary: {
          DEFAULT: 'hsl(var(--secondary))',
          foreground: 'hsl(var(--secondary-foreground))',
        },
        destructive: {
          DEFAULT: 'hsl(var(--destructive))',
          foreground: 'hsl(var(--destructive-foreground))',
        },
        muted: {
          DEFAULT: 'hsl(var(--muted))',
          foreground: 'hsl(var(--muted-foreground))',
        },
        accent: {
          DEFAULT: 'hsl(var(--accent))',
          foreground: 'hsl(var(--accent-foreground))',
        },
        popover: {
          DEFAULT: 'hsl(var(--popover))',
          foreground: 'hsl(var(--popover-foreground))',
        },
        card: {
          DEFAULT: 'hsl(var(--card))',
          foreground: 'hsl(var(--card-foreground))',
        },
        sidebar: {
          DEFAULT: 'hsl(var(--sidebar))',
          foreground: 'hsl(var(--sidebar-foreground))',
          accent: 'hsl(var(--sidebar-accent))',
          border: 'hsl(var(--sidebar-border))',
        },
      },
      borderRadius: {
        lg: 'var(--radius)',
        md: 'calc(var(--radius) - 2px)',
        sm: 'calc(var(--radius) - 4px)',
      },
      // Motion tokens — see src/renderer/styles/motion-tokens.ts for the
      // single source of truth. The utilities resolve to CSS variables so
      // the [data-motion="reduced"] override in globals.css can rescale
      // them at runtime; raw ms values remain in motion-tokens.ts for any
      // imperative JS that needs the numbers.
      // 动画 token：source of truth 是 src/renderer/styles/motion-tokens.ts。
      // utility 解析到 CSS 变量，让 globals.css 的 reduced 档可以在运行时
      // 缩放它们；motion-tokens.ts 保留具体毫秒值供命令式 JS 使用。
      transitionDuration: {
        instant: 'var(--motion-duration-instant)',
        quick: 'var(--motion-duration-quick)',
        base: 'var(--motion-duration-base)',
        smooth: 'var(--motion-duration-smooth)',
        slow: 'var(--motion-duration-slow)',
      },
      transitionTimingFunction: {
        standard: 'var(--motion-easing-standard)',
        enter: 'var(--motion-easing-enter)',
        exit: 'var(--motion-easing-exit)',
        emphasized: 'var(--motion-easing-emphasized)',
      },
      scale: {
        'press-in': '0.95',
        'enter-from': '0.96',
        'hover-lift': '1.02',
        'media-zoom': '1.08',
      },
      animationDuration: {
        instant: 'var(--motion-duration-instant)',
        quick: 'var(--motion-duration-quick)',
        base: 'var(--motion-duration-base)',
        smooth: 'var(--motion-duration-smooth)',
        slow: 'var(--motion-duration-slow)',
      },
      animationTimingFunction: {
        standard: 'var(--motion-easing-standard)',
        enter: 'var(--motion-easing-enter)',
        exit: 'var(--motion-easing-exit)',
        emphasized: 'var(--motion-easing-emphasized)',
      },
    },
  },
  plugins: [require('tailwindcss-animate')],
};
