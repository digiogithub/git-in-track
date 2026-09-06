import { forwardRef, type SelectHTMLAttributes } from 'react';

import { fieldClasses } from '@/components/ui/field';
import { cn } from '@/lib/cn';

export type SelectProps = SelectHTMLAttributes<HTMLSelectElement>;

/**
 * Styled native `<select>`.
 *
 * A native control is deliberate: it is keyboard and screen-reader correct on
 * every platform, works inside a virtualised table row, and needs no portal.
 * Rich multi-value pickers are built in the feature from checkbox groups.
 *
 * The chevron is painted as a background image rather than an overlaid icon so
 * the control stays a single element. A `background-image` cannot inherit
 * `currentColor`, and a `dark:` utility would not fire in system-dark (the
 * theme is a token swap, not a class), so this is the one hardcoded colour in
 * the system: a mid warm neutral measured at 3.5:1 on the light field and
 * 4.0:1 on the dark one.
 */
export const Select = forwardRef<HTMLSelectElement, SelectProps>(function Select(
  { className, style, ...props },
  ref,
) {
  return (
    <select
      ref={ref}
      className={cn(
        fieldClasses,
        // The two arbitrary values carry an explicit type hint: without it
        // `tailwind-merge` reads them as background *colours* and drops the
        // field's own `bg-*` class, leaving the control with the UA grey.
        'h-9 appearance-none bg-[size:1rem] bg-[position:right_0.5rem_center] bg-no-repeat py-0 pl-2.5 pr-8',
        className,
      )}
      style={{
        backgroundImage:
          "url(\"data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%238A8072' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'%3E%3Cpath d='m6 9 6 6 6-6'/%3E%3C/svg%3E\")",
        ...style,
      }}
      {...props}
    />
  );
});
