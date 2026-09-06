import { cn } from '@/lib/cn';

/**
 * The brand mark: a commit graph — two commits on a branch, one merge point.
 * It is drawn with `currentColor` and two token tints so it works on any
 * surface and in both themes; the same shape is the favicon.
 */
export function Logo({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 32 32"
      role="img"
      aria-label="git-in-track"
      className={cn('h-5 w-5', className)}
    >
      <path
        d="M10 12v8M12.6 21.2 19.4 17.4M12.6 10.8 19.4 14.6"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        fill="none"
        opacity="0.45"
      />
      <circle cx="10" cy="9" r="3" fill="currentColor" opacity="0.55" />
      <circle cx="10" cy="23" r="3" fill="currentColor" opacity="0.55" />
      <circle cx="22" cy="16" r="3.5" fill="hsl(var(--accent))" />
    </svg>
  );
}
