/**
 * The one field skin, shared by every text-entry control.
 *
 * A control is an inset well, not a raised slab: it sits a step *below* the
 * surface it lives on in both themes, which is what makes a form scannable at
 * a glance. Its resting border is the only line in the system allowed to be
 * stronger than a separator (WCAG 1.4.11 asks a control's boundary to clear
 * 3:1), and focus is the copper ring, never a colour change.
 */
export const fieldClasses =
  'w-full rounded-md border border-input/60 bg-surface-muted/50 text-sm text-foreground shadow-xs transition-colors duration-fast ease-out placeholder:text-subtle-foreground hover:border-input focus-visible:border-input focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-not-allowed disabled:opacity-50 aria-[invalid=true]:border-destructive aria-[invalid=true]:focus-visible:ring-destructive';
