import {
  forwardRef,
  type HTMLAttributes,
  type TdHTMLAttributes,
  type ThHTMLAttributes,
} from 'react';

import { cn } from '@/lib/cn';

/**
 * Unstyled-ish table primitives; the data grid logic lives in the feature.
 *
 * Density is the whole design here: a 2rem header, 2.25rem rows, hairline
 * separators and a hover that tints rather than boxes. Zebra striping is
 * deliberately absent — with separators this soft it would add a second,
 * competing rhythm.
 */
export const Table = forwardRef<HTMLTableElement, HTMLAttributes<HTMLTableElement>>(function Table(
  { className, ...props },
  ref,
) {
  return (
    <div className="relative w-full overflow-x-auto">
      <table
        ref={ref}
        className={cn('w-full caption-bottom border-collapse text-sm', className)}
        {...props}
      />
    </div>
  );
});

export const TableHeader = forwardRef<
  HTMLTableSectionElement,
  HTMLAttributes<HTMLTableSectionElement>
>(function TableHeader({ className, ...props }, ref) {
  return (
    <thead ref={ref} className={cn('[&_tr]:border-b [&_tr]:border-border', className)} {...props} />
  );
});

export const TableBody = forwardRef<
  HTMLTableSectionElement,
  HTMLAttributes<HTMLTableSectionElement>
>(function TableBody({ className, ...props }, ref) {
  return <tbody ref={ref} className={cn('[&_tr:last-child]:border-0', className)} {...props} />;
});

export const TableRow = forwardRef<HTMLTableRowElement, HTMLAttributes<HTMLTableRowElement>>(
  function TableRow({ className, ...props }, ref) {
    return (
      <tr
        ref={ref}
        className={cn(
          'border-b border-border/70 transition-colors duration-fast hover:bg-surface-muted/60 data-[selected=true]:bg-accent-subtle/60',
          className,
        )}
        {...props}
      />
    );
  },
);

export const TableHead = forwardRef<HTMLTableCellElement, ThHTMLAttributes<HTMLTableCellElement>>(
  function TableHead({ className, ...props }, ref) {
    return (
      <th
        ref={ref}
        className={cn(
          'h-8 whitespace-nowrap px-3 text-left align-middle text-2xs font-medium uppercase tracking-[0.08em] text-subtle-foreground',
          className,
        )}
        {...props}
      />
    );
  },
);

export const TableCell = forwardRef<HTMLTableCellElement, TdHTMLAttributes<HTMLTableCellElement>>(
  function TableCell({ className, ...props }, ref) {
    return <td ref={ref} className={cn('px-3 py-2 align-middle', className)} {...props} />;
  },
);

export const TableCaption = forwardRef<
  HTMLTableCaptionElement,
  HTMLAttributes<HTMLTableCaptionElement>
>(function TableCaption({ className, ...props }, ref) {
  return (
    <caption ref={ref} className={cn('mt-3 text-sm text-muted-foreground', className)} {...props} />
  );
});
