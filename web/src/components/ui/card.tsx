import { forwardRef, type HTMLAttributes } from 'react';

import { cn } from '@/lib/cn';

export type CardProps = HTMLAttributes<HTMLDivElement>;

/**
 * A card is one step of elevation over the page: a lighter plane, a hairline
 * border and the softest of the shadow tokens. Nesting cards is not a pattern
 * here — a group inside a card is a `bg-surface-muted` well instead.
 */
export const Card = forwardRef<HTMLDivElement, CardProps>(function Card(
  { className, ...props },
  ref,
) {
  return (
    <div
      ref={ref}
      className={cn(
        'rounded-lg border border-border bg-card text-card-foreground shadow-card',
        className,
      )}
      {...props}
    />
  );
});

export const CardHeader = forwardRef<HTMLDivElement, CardProps>(function CardHeader(
  { className, ...props },
  ref,
) {
  return (
    <div ref={ref} className={cn('flex flex-col gap-1.5 px-5 pb-4 pt-5', className)} {...props} />
  );
});

export const CardTitle = forwardRef<HTMLHeadingElement, HTMLAttributes<HTMLHeadingElement>>(
  function CardTitle({ className, ...props }, ref) {
    return (
      <h3
        ref={ref}
        className={cn('text-sm font-semibold leading-none tracking-tight', className)}
        {...props}
      />
    );
  },
);

export const CardDescription = forwardRef<
  HTMLParagraphElement,
  HTMLAttributes<HTMLParagraphElement>
>(function CardDescription({ className, ...props }, ref) {
  return (
    <p
      ref={ref}
      className={cn('text-sm leading-relaxed text-muted-foreground', className)}
      {...props}
    />
  );
});

export const CardContent = forwardRef<HTMLDivElement, CardProps>(function CardContent(
  { className, ...props },
  ref,
) {
  return <div ref={ref} className={cn('px-5 pb-5', className)} {...props} />;
});

export const CardFooter = forwardRef<HTMLDivElement, CardProps>(function CardFooter(
  { className, ...props },
  ref,
) {
  return (
    <div
      ref={ref}
      className={cn('flex items-center gap-2 border-t border-border px-5 py-3.5', className)}
      {...props}
    />
  );
});
