import { forwardRef, type InputHTMLAttributes } from 'react';

import { fieldClasses } from '@/components/ui/field';
import { cn } from '@/lib/cn';

export type InputProps = InputHTMLAttributes<HTMLInputElement>;

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { className, type = 'text', ...props },
  ref,
) {
  return (
    <input
      ref={ref}
      type={type}
      className={cn(fieldClasses, 'flex h-9 px-3 py-1', className)}
      {...props}
    />
  );
});
