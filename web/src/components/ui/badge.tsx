import { cva, type VariantProps } from 'class-variance-authority';
import { forwardRef, type HTMLAttributes } from 'react';

import { cn } from '@/lib/cn';

/**
 * Small status/label chip. Every tone is a tint of a semantic token over the
 * current surface plus that token as the text colour, so a theme swap never
 * needs a component change and every chip clears 4.5:1 against its own tint.
 * `solid` is reserved for the one state that must outrank the others on a row
 * (critical priority); using it twice on the same row defeats the point.
 */
export const badgeVariants = cva(
  'inline-flex items-center gap-1 whitespace-nowrap rounded-full border px-2 py-0.5 text-xs font-medium transition-colors duration-fast [&_svg]:h-3 [&_svg]:w-3',
  {
    variants: {
      variant: {
        default: 'border-transparent bg-secondary text-secondary-foreground',
        outline: 'border-border-strong text-muted-foreground',
        accent: 'border-transparent bg-accent/15 text-accent',
        destructive: 'border-transparent bg-destructive/15 text-destructive',
        success: 'border-transparent bg-success/15 text-success',
        warning: 'border-transparent bg-warning/15 text-warning',
        info: 'border-transparent bg-info/15 text-info',
        solid: 'border-transparent bg-destructive text-destructive-foreground',
      },
      size: {
        default: 'px-2 py-0.5 text-xs',
        sm: 'px-1.5 py-0 text-2xs',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  },
);

export type BadgeProps = HTMLAttributes<HTMLSpanElement> & VariantProps<typeof badgeVariants>;

export const Badge = forwardRef<HTMLSpanElement, BadgeProps>(function Badge(
  { className, variant, size, ...props },
  ref,
) {
  return <span ref={ref} className={cn(badgeVariants({ variant, size }), className)} {...props} />;
});
