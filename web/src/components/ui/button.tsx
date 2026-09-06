import { cva, type VariantProps } from 'class-variance-authority';
import { forwardRef, type ButtonHTMLAttributes } from 'react';

import { cn } from '@/lib/cn';

/**
 * Buttons carry weight, and weight is the hierarchy: exactly one `accent`
 * button per view (the thing the screen is for), `default` for the neutral
 * commits around it, `outline` and `ghost` for everything else. Colour is not
 * the hierarchy — a screen full of copper buttons has none.
 */
export const buttonVariants = cva(
  'inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-colors duration-fast ease-out focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:pointer-events-none disabled:opacity-50 [&_svg]:shrink-0',
  {
    variants: {
      variant: {
        default: 'bg-primary text-primary-foreground shadow-xs hover:bg-primary/90',
        accent: 'bg-accent text-accent-foreground shadow-xs hover:bg-accent-hover',
        destructive: 'bg-destructive text-destructive-foreground shadow-xs hover:bg-destructive/90',
        outline:
          'border border-border-strong bg-surface text-foreground shadow-xs hover:border-input hover:bg-secondary',
        secondary: 'bg-secondary text-secondary-foreground hover:bg-secondary/70',
        ghost: 'text-muted-foreground hover:bg-secondary hover:text-foreground',
        link: 'text-accent underline-offset-4 hover:text-accent-hover hover:underline',
      },
      size: {
        default: 'h-9 px-3.5 py-2',
        sm: 'h-8 rounded-sm px-3 text-xs',
        lg: 'h-10 px-5',
        icon: 'h-9 w-9',
        'icon-sm': 'h-8 w-8 rounded-sm',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  },
);

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> &
  VariantProps<typeof buttonVariants>;

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { className, variant, size, type = 'button', ...props },
  ref,
) {
  return (
    <button
      ref={ref}
      type={type}
      className={cn(buttonVariants({ variant, size }), className)}
      {...props}
    />
  );
});
