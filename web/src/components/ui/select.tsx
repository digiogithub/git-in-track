import { forwardRef, type SelectHTMLAttributes } from 'react';

import { cn } from '@/lib/cn';

export type SelectProps = SelectHTMLAttributes<HTMLSelectElement>;

/**
 * Styled native `<select>`.
 *
 * A native control is deliberate: it is keyboard and screen-reader correct on
 * every platform, works inside a virtualised table row, and needs no portal.
 * Rich multi-value pickers are built in the feature from checkbox groups.
 */
export const Select = forwardRef<HTMLSelectElement, SelectProps>(function Select(
  { className, ...props },
  ref,
) {
  return (
    <select
      ref={ref}
      className={cn(
        'h-9 w-full rounded-md border border-input bg-background px-2 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50',
        className,
      )}
      {...props}
    />
  );
});
