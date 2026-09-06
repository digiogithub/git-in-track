import { forwardRef, type TextareaHTMLAttributes } from 'react';

import { fieldClasses } from '@/components/ui/field';
import { cn } from '@/lib/cn';

export type TextareaProps = TextareaHTMLAttributes<HTMLTextAreaElement>;

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(function Textarea(
  { className, ...props },
  ref,
) {
  return (
    <textarea
      ref={ref}
      className={cn(
        fieldClasses,
        'flex min-h-[6rem] px-3 py-2 font-mono leading-relaxed',
        className,
      )}
      {...props}
    />
  );
});
