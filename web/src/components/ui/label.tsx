import { forwardRef, type LabelHTMLAttributes } from 'react';

import { cn } from '@/lib/cn';

export type LabelProps = LabelHTMLAttributes<HTMLLabelElement>;

/**
 * Field label: small caps with open tracking. It reads as metadata rather than
 * as prose, which is what keeps a dense form from looking like a paragraph.
 */
export const Label = forwardRef<HTMLLabelElement, LabelProps>(function Label(
  { className, ...props },
  ref,
) {
  return (
    <label
      ref={ref}
      className={cn(
        'text-2xs font-medium uppercase tracking-[0.08em] text-muted-foreground',
        className,
      )}
      {...props}
    />
  );
});
