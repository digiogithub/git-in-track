import type { Config } from 'tailwindcss';

/**
 * Design tokens are CSS custom properties declared in `src/index.css`, so the
 * light/dark/system theming is a token swap and never a class-by-class rewrite.
 * Never hardcode a colour in a component: add a token there and expose it here.
 *
 * Everything below is a *name for a token*, not a value. The one exception is
 * the type scale, which is measured in rem and does not change with the theme.
 * See docs/13-design-system.md.
 */
export default {
  darkMode: ['class', '[data-theme="dark"]'],
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    container: {
      center: true,
      padding: '1.5rem',
      screens: { '2xl': '1400px' },
    },
    extend: {
      colors: {
        background: 'hsl(var(--background) / <alpha-value>)',
        foreground: 'hsl(var(--foreground) / <alpha-value>)',
        /* Planes, from the page up to an overlay. */
        surface: {
          DEFAULT: 'hsl(var(--surface) / <alpha-value>)',
          muted: 'hsl(var(--surface-muted) / <alpha-value>)',
        },
        elevated: 'hsl(var(--elevated) / <alpha-value>)',
        sidebar: {
          DEFAULT: 'hsl(var(--sidebar) / <alpha-value>)',
          border: 'hsl(var(--sidebar-border) / <alpha-value>)',
        },
        card: {
          DEFAULT: 'hsl(var(--card) / <alpha-value>)',
          foreground: 'hsl(var(--card-foreground) / <alpha-value>)',
        },
        popover: {
          DEFAULT: 'hsl(var(--popover) / <alpha-value>)',
          foreground: 'hsl(var(--popover-foreground) / <alpha-value>)',
        },
        primary: {
          DEFAULT: 'hsl(var(--primary) / <alpha-value>)',
          foreground: 'hsl(var(--primary-foreground) / <alpha-value>)',
        },
        secondary: {
          DEFAULT: 'hsl(var(--secondary) / <alpha-value>)',
          foreground: 'hsl(var(--secondary-foreground) / <alpha-value>)',
        },
        muted: {
          DEFAULT: 'hsl(var(--muted) / <alpha-value>)',
          foreground: 'hsl(var(--muted-foreground) / <alpha-value>)',
        },
        'subtle-foreground': 'hsl(var(--subtle-foreground) / <alpha-value>)',
        accent: {
          DEFAULT: 'hsl(var(--accent) / <alpha-value>)',
          foreground: 'hsl(var(--accent-foreground) / <alpha-value>)',
          hover: 'hsl(var(--accent-hover) / <alpha-value>)',
          subtle: 'hsl(var(--accent-subtle) / <alpha-value>)',
        },
        destructive: {
          DEFAULT: 'hsl(var(--destructive) / <alpha-value>)',
          foreground: 'hsl(var(--destructive-foreground) / <alpha-value>)',
        },
        success: 'hsl(var(--success) / <alpha-value>)',
        warning: 'hsl(var(--warning) / <alpha-value>)',
        info: 'hsl(var(--info) / <alpha-value>)',
        border: {
          DEFAULT: 'hsl(var(--border) / <alpha-value>)',
          strong: 'hsl(var(--border-strong) / <alpha-value>)',
        },
        input: 'hsl(var(--input) / <alpha-value>)',
        ring: 'hsl(var(--ring) / <alpha-value>)',
        overlay: 'hsl(var(--overlay) / <alpha-value>)',
        code: 'hsl(var(--code) / <alpha-value>)',
      },
      borderRadius: {
        lg: 'var(--radius-lg)',
        md: 'var(--radius)',
        sm: 'var(--radius-sm)',
      },
      boxShadow: {
        /* Named for intent, not for size: `card` sits on the page, `pop` floats
           over it, `overlay` is a dialog. Raw `shadow-xl` never appears. */
        xs: 'var(--shadow-xs)',
        card: 'var(--shadow-sm)',
        raised: 'var(--shadow-md)',
        pop: 'var(--shadow-md)',
        overlay: 'var(--shadow-lg)',
      },
      fontFamily: {
        sans: ['var(--font-sans)'],
        mono: ['var(--font-mono)'],
      },
      fontSize: {
        /* A dense tool needs a step below `text-xs` that is still legible: it is
           only ever used for uppercase meta labels, never for prose. */
        '2xs': ['0.6875rem', { lineHeight: '1rem', letterSpacing: '0.02em' }],
      },
      transitionTimingFunction: {
        out: 'var(--ease-out)',
      },
      transitionDuration: {
        fast: 'var(--duration-fast)',
        DEFAULT: 'var(--duration)',
      },
      keyframes: {
        'fade-in': {
          from: { opacity: '0' },
          to: { opacity: '1' },
        },
        'scale-in': {
          from: { opacity: '0', transform: 'translate(-50%, -48%) scale(0.98)' },
          to: { opacity: '1', transform: 'translate(-50%, -50%) scale(1)' },
        },
        'slide-in': {
          from: { opacity: '0', transform: 'translateY(0.375rem)' },
          to: { opacity: '1', transform: 'translateY(0)' },
        },
      },
      animation: {
        'fade-in': 'fade-in var(--duration) var(--ease-out)',
        'scale-in': 'scale-in var(--duration) var(--ease-out)',
        'slide-in': 'slide-in var(--duration) var(--ease-out)',
      },
    },
  },
  plugins: [],
} satisfies Config;
