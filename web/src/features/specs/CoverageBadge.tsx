import { CircleAlert, CircleCheck, CircleDashed, CircleHelp, CircleX } from 'lucide-react';
import type { ReactNode } from 'react';

import { Badge, type BadgeProps } from '@/components/ui/badge';
import type { CoverageState } from '@/features/specs/search';

type Shown = CoverageState | 'unavailable';

/**
 * Tone, icon and wording per state. The state is always spelled out: colour and
 * icon only reinforce it (docs/13-design-system.md, accessibility).
 */
const looks: Record<Shown, { variant: BadgeProps['variant']; icon: ReactNode; hint: string }> = {
  passing: {
    variant: 'success',
    icon: <CircleCheck aria-hidden="true" />,
    hint: 'Every linked test passed on the current text.',
  },
  failing: {
    variant: 'destructive',
    icon: <CircleX aria-hidden="true" />,
    hint: 'A linked test failed.',
  },
  suspect: {
    variant: 'warning',
    icon: <CircleAlert aria-hidden="true" />,
    hint: 'The requirement or the code it traces changed since it was verified.',
  },
  untested: {
    variant: 'default',
    icon: <CircleDashed aria-hidden="true" />,
    hint: 'No linked test, or no result for one yet.',
  },
  unavailable: {
    variant: 'outline',
    icon: <CircleHelp aria-hidden="true" />,
    hint: 'Coverage needs the companion: run `gintrack serve`.',
  },
};

/** The computed coverage of one requirement (doc 03 §21.6), or `unavailable`. */
export function CoverageBadge({ state }: { state: Shown | undefined }) {
  if (state === undefined) {
    return <span className="text-xs text-muted-foreground">…</span>;
  }
  const look = looks[state];
  return (
    <Badge variant={look.variant} title={look.hint} data-coverage={state}>
      {look.icon}
      {state}
    </Badge>
  );
}
