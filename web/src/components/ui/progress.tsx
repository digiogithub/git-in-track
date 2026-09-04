import { forwardRef, type HTMLAttributes } from 'react';

import { cn } from '@/lib/cn';

export type ProgressProps = Omit<HTMLAttributes<HTMLDivElement>, 'children'> & {
  /** Completed amount. */
  value: number;
  /** Total amount; `0` renders an empty, non-erroring bar. */
  max?: number;
  /** Accessible name; falls back to a generic one. */
  label?: string;
  /** Extra classes for the filled part (a semantic token, never a raw colour). */
  indicatorClassName?: string;
};

/** Determinate progress bar with an explicit accessible name. */
export const Progress = forwardRef<HTMLDivElement, ProgressProps>(function Progress(
  { className, value, max = 100, label, indicatorClassName, ...props },
  ref,
) {
  const safeMax = max > 0 ? max : 0;
  const percent = safeMax === 0 ? 0 : Math.min(100, Math.round((value / safeMax) * 100));

  return (
    <div
      ref={ref}
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={safeMax}
      aria-valuenow={value}
      aria-valuetext={`${percent}%`}
      aria-label={label ?? 'Progress'}
      className={cn('h-2 w-full overflow-hidden rounded-full bg-secondary', className)}
      {...props}
    >
      <div
        className={cn('h-full rounded-full bg-accent transition-[width]', indicatorClassName)}
        style={{ width: `${percent}%` }}
      />
    </div>
  );
});
