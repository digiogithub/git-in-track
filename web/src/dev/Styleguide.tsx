/**
 * The design system, rendered by the components it documents.
 *
 * A written spec drifts the moment someone edits a variant; this page cannot,
 * because every swatch reads a live token and every control below is the same
 * component the app imports. If something here looks wrong, the app is wrong.
 *
 * Dev-only: `npm run styleguide`. See docs/13-design-system.md.
 */

import { AlertTriangle, GitBranch, Info, Lock, Plus, Search, Trash2 } from 'lucide-react';
import { useState, type ReactNode } from 'react';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Logo } from '@/components/ui/logo';
import { Progress } from '@/components/ui/progress';
import { Select } from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Textarea } from '@/components/ui/textarea';
import { ThemeToggle } from '@/components/ui/theme-toggle';
import { ToastProvider, useToast } from '@/components/ui/toast';
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip';
import { tokenContrast } from '@/dev/contrast';
import { priorityBadgeClass, statusBadgeClass } from '@/features/backlog/item-meta';
import { cn } from '@/lib/cn';

function Section({
  title,
  intent,
  children,
}: {
  title: string;
  intent: string;
  children: ReactNode;
}) {
  return (
    <section className="space-y-4 border-t border-border pt-8">
      <header className="space-y-1">
        <h2 className="text-base font-semibold tracking-tight">{title}</h2>
        <p className="max-w-[65ch] text-sm text-muted-foreground">{intent}</p>
      </header>
      {children}
    </section>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center gap-3 py-2">
      <span className="w-28 shrink-0 text-2xs uppercase tracking-[0.08em] text-subtle-foreground">
        {label}
      </span>
      <div className="flex flex-wrap items-center gap-2">{children}</div>
    </div>
  );
}

/** A surface swatch: the plane itself, its token, and what it is for. */
function Surface({ token, name, use }: { token: string; name: string; use: string }) {
  return (
    <div className="flex items-center gap-3">
      <span
        className="h-10 w-10 shrink-0 rounded-md border border-border-strong"
        style={{ background: `hsl(var(${token}))` }}
      />
      <div className="min-w-0">
        <p className="font-mono text-xs">{token}</p>
        <p className="text-xs text-muted-foreground">
          {name} — {use}
        </p>
      </div>
    </div>
  );
}

/** A text/ink swatch that measures itself against the surface it sits on. */
function Ink({ token, on, use }: { token: string; on: string; use: string }) {
  const measured = tokenContrast(token, on);
  return (
    <div className="flex items-center justify-between gap-4 rounded-md border border-border px-3 py-2">
      <div className="min-w-0">
        <p className="truncate text-sm font-medium" style={{ color: `hsl(var(${token}))` }}>
          The quick brown fox
        </p>
        <p className="font-mono text-2xs text-subtle-foreground">
          {token} on {on}
        </p>
      </div>
      <div className="shrink-0 text-right">
        <p className="text-sm tabular-nums">{measured ? measured.ratio.toFixed(2) : '—'}</p>
        <p className="text-2xs uppercase tracking-[0.08em] text-subtle-foreground">
          {measured?.grade ?? 'unset'}
        </p>
        <span className="sr-only">{use}</span>
      </div>
    </div>
  );
}

function Chip({ token, label }: { token: string; label: string }) {
  return (
    <span
      className="inline-flex items-center gap-1.5 rounded-full border border-transparent px-2 py-0.5 text-xs font-medium"
      style={{ background: `hsl(var(${token}) / 0.14)`, color: `hsl(var(${token}))` }}
    >
      <span className="h-2 w-2 rounded-full" style={{ background: `hsl(var(${token}))` }} />
      {label}
    </span>
  );
}

function StyleguideBody() {
  const { toast } = useToast();
  const [checked, setChecked] = useState(true);

  return (
    <div className="mx-auto max-w-[64rem] space-y-10 px-6 py-10">
      <header className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center gap-2.5">
            <Logo className="h-7 w-7" />
            <div>
              <h1 className="page-title">Design system</h1>
              <p className="page-subtitle">git-in-track — warm paper, soft contrast</p>
            </div>
          </div>
          <ThemeToggle />
        </div>
        <p className="max-w-[70ch] text-sm leading-relaxed text-muted-foreground">
          Every colour on this page is a live token: switch the theme above and the swatches, the
          components and the measured contrast ratios all follow. The palette is deliberately soft —
          no pure white, no pure black — because this is a tool people keep open all day, but the
          softness is spent on backgrounds only: text is measured, and the numbers are shown.
        </p>
      </header>

      <Section
        title="Surfaces"
        intent="Four planes, warm and close together. Elevation is carried by a hairline border and a soft shadow, not by a bright slab; nesting a card inside a card is not a pattern here — a group inside a card is a muted well."
      >
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <Surface token="--background" name="Page" use="the ground everything sits on" />
          <Surface token="--sidebar" name="Sidebar" use="navigation, one step off the page" />
          <Surface token="--surface" name="Card" use="panels and cards" />
          <Surface token="--surface-muted" name="Well" use="inset rows, fields, code" />
          <Surface token="--elevated" name="Overlay" use="dialogs, popovers, toasts" />
          <Surface token="--code" name="Code" use="fenced blocks and inline code" />
        </div>
      </Section>

      <Section
        title="Ink"
        intent="Three weights of text and one rule: 4.5:1 or it is not text. The ratios below are computed from the rendered tokens, so a palette edit shows up here before it ships."
      >
        <div className="grid gap-2 sm:grid-cols-2">
          <Ink token="--foreground" on="--background" use="body and headings" />
          <Ink token="--muted-foreground" on="--background" use="secondary copy" />
          <Ink token="--subtle-foreground" on="--background" use="meta labels, placeholders" />
          <Ink token="--accent" on="--background" use="links and active state" />
          <Ink token="--destructive" on="--background" use="errors" />
          <Ink token="--success" on="--background" use="confirmations" />
          <Ink token="--warning" on="--background" use="stale, needs attention" />
          <Ink token="--info" on="--background" use="neutral notices" />
        </div>
      </Section>

      <Section
        title="Accent"
        intent="One accent, copper, and it means “this is where you are or what you can do”. It is never a data-series colour, so a mark in a chart can never be mistaken for a link."
      >
        <div className="flex flex-wrap items-center gap-3">
          <span className="h-12 w-24 rounded-md bg-accent" />
          <span className="h-12 w-24 rounded-md bg-accent-hover" />
          <span className="h-12 w-24 rounded-md bg-accent-subtle" />
          <span className="h-12 w-24 rounded-md border border-border bg-accent/15" />
          <a className="text-sm text-accent underline underline-offset-4" href="#accent">
            A link in running text
          </a>
        </div>
      </Section>

      <Section
        title="Status and priority"
        intent="Read one at a time, as a tinted chip, so each only has to clear 4.5:1 against its own tint. Priority separates critical from high by fill, not by hue alone: colour is never the only signal."
      >
        <Row label="Status">
          {(['todo', 'in_progress', 'done', 'cancelled', 'unknown'] as const).map((category) => (
            <Badge key={category} variant="outline" className={statusBadgeClass(category)}>
              {category.replace('_', ' ')}
            </Badge>
          ))}
        </Row>
        <Row label="Priority">
          <Badge variant="solid">critical</Badge>
          {(['high', 'medium', 'low'] as const).map((priority) => (
            <Badge key={priority} variant="outline" className={priorityBadgeClass(priority)}>
              {priority}
            </Badge>
          ))}
        </Row>
        <Row label="Chart series">
          <Chip token="--chart-todo" label="To do" />
          <Chip token="--chart-progress" label="In progress" />
          <Chip token="--chart-done" label="Done" />
          <Chip token="--chart-cancelled" label="Cancelled" />
        </Row>
      </Section>

      <Section
        title="Type"
        intent="One family, four sizes and two weights. Hierarchy comes from size and colour, never from more fonts."
      >
        <div className="space-y-2">
          <p className="page-title">Page title — 20px semibold</p>
          <p className="text-base font-semibold tracking-tight">Section — 16px semibold</p>
          <p className="text-sm">Body — 14px regular, the size most of the app is set in</p>
          <p className="text-xs text-muted-foreground">Small — 12px, secondary information</p>
          <p className="section-label">Label — 11px uppercase, metadata only</p>
          <p className="font-mono text-sm">Mono — identifiers, paths, ACME-US-0042</p>
        </div>
      </Section>

      <Section
        title="Buttons"
        intent="Weight is the hierarchy: exactly one accent button per view, neutral for the commits around it, outline and ghost for everything else."
      >
        <Row label="Variants">
          <Button variant="accent">
            <Plus aria-hidden="true" className="h-4 w-4" />
            New item
          </Button>
          <Button>Save</Button>
          <Button variant="secondary">Secondary</Button>
          <Button variant="outline">Outline</Button>
          <Button variant="ghost">Ghost</Button>
          <Button variant="destructive">
            <Trash2 aria-hidden="true" className="h-4 w-4" />
            Delete
          </Button>
          <Button variant="link">Link</Button>
        </Row>
        <Row label="Sizes">
          <Button size="lg">Large</Button>
          <Button>Default</Button>
          <Button size="sm">Small</Button>
          <Button size="icon" aria-label="Search">
            <Search aria-hidden="true" className="h-4 w-4" />
          </Button>
          <Button size="icon-sm" variant="ghost" aria-label="Search">
            <Search aria-hidden="true" className="h-4 w-4" />
          </Button>
        </Row>
        <Row label="States">
          <Button disabled>Disabled</Button>
          <Button variant="outline" disabled>
            Disabled
          </Button>
        </Row>
      </Section>

      <Section
        title="Fields"
        intent="A control is an inset well, not a raised slab: it sits a step below the surface it lives on, in both themes. Focus is always the copper ring, never a colour change."
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-1.5">
            <Label htmlFor="sg-input">Title</Label>
            <Input id="sg-input" placeholder="Login with SSO" />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="sg-select">Status</Label>
            <Select id="sg-select" defaultValue="in_progress">
              <option value="todo">To do</option>
              <option value="in_progress">In progress</option>
              <option value="done">Done</option>
            </Select>
          </div>
          <div className="space-y-1.5 sm:col-span-2">
            <Label htmlFor="sg-textarea">Body</Label>
            <Textarea id="sg-textarea" defaultValue={'## Acceptance criteria\n- [ ] It works'} />
          </div>
        </div>
        <Row label="Toggles">
          <Switch checked={checked} onCheckedChange={setChecked} aria-label="Commit each save" />
          <label className="flex items-center gap-2 text-sm" htmlFor="sg-checked">
            <Checkbox id="sg-checked" defaultChecked /> Selected
          </label>
          <label className="flex items-center gap-2 text-sm" htmlFor="sg-mixed">
            <Checkbox id="sg-mixed" indeterminate /> Mixed
          </label>
        </Row>
        <Row label="Progress">
          <Progress value={62} label="Acceptance criteria" className="w-56" />
          <span className="text-xs text-muted-foreground">62%</span>
        </Row>
      </Section>

      <Section
        title="Containers"
        intent="A card is one step of elevation and one hairline. Banners are strips that belong to the page they explain, never floating cards."
      >
        <div className="grid items-start gap-4 lg:grid-cols-2">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <GitBranch aria-hidden="true" className="h-4 w-4 text-accent" />
                Sprint 12
              </CardTitle>
              <CardDescription>Ends in four days — 18 of 26 points done.</CardDescription>
            </CardHeader>
            <CardContent className="space-y-3">
              <Progress value={18} max={26} label="Sprint progress" />
              <div className="flex flex-wrap gap-1.5">
                <Badge variant="outline" className={statusBadgeClass('in_progress')}>
                  in progress
                </Badge>
                <Badge variant="outline">story</Badge>
                <Badge variant="accent">frontend</Badge>
              </div>
            </CardContent>
          </Card>

          <div className="space-y-3">
            <div className="flex items-start gap-3 rounded-md border border-border bg-surface-muted/70 px-4 py-2.5 text-sm">
              <Info aria-hidden="true" className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
              <p className="flex-1">
                <strong className="font-medium">Companion detected.</strong> Native indexing and
                file watching enabled.
              </p>
            </div>
            <div className="flex items-start gap-3 rounded-md border border-destructive/30 bg-destructive/10 px-4 py-2.5 text-sm text-destructive">
              <Lock aria-hidden="true" className="mt-0.5 h-4 w-4 shrink-0" />
              <p className="flex-1">
                <strong className="font-medium">The companion needs an access token.</strong> Paste
                it in Settings.
              </p>
            </div>
            <div className="flex items-start gap-3 rounded-md border border-warning/30 bg-warning/10 px-4 py-2.5 text-sm text-warning">
              <AlertTriangle aria-hidden="true" className="mt-0.5 h-4 w-4 shrink-0" />
              <p className="flex-1">
                <strong className="font-medium">Snapshot is 6 days old.</strong> Re-run the sync to
                refresh the remote board.
              </p>
            </div>
            <p className="empty-state">Nothing links here yet.</p>
          </div>
        </div>
      </Section>

      <Section
        title="Table"
        intent="The densest surface in the app: a 2rem header, hairline separators, a hover that tints rather than boxes, and no zebra striping — with separators this soft it would add a second, competing rhythm."
      >
        <div className="overflow-hidden rounded-lg border border-border bg-card shadow-card">
          <Table>
            <TableHeader className="bg-surface-muted/50">
              <TableRow>
                <TableHead>Id</TableHead>
                <TableHead>Title</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Priority</TableHead>
                <TableHead>Points</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {[
                {
                  id: 'ACME-US-0042',
                  title: 'Login with SSO',
                  status: 'in_progress',
                  priority: 'high',
                  points: 5,
                },
                {
                  id: 'ACME-US-0043',
                  title: 'Rotate signing keys',
                  status: 'todo',
                  priority: 'critical',
                  points: 3,
                },
                {
                  id: 'ACME-TA-0107',
                  title: 'Cache the index between runs',
                  status: 'done',
                  priority: 'medium',
                  points: 2,
                },
              ].map((row) => (
                <TableRow key={row.id}>
                  <TableCell className="font-mono text-xs text-accent">{row.id}</TableCell>
                  <TableCell>{row.title}</TableCell>
                  <TableCell>
                    <Badge
                      variant="outline"
                      className={statusBadgeClass(row.status as 'todo' | 'in_progress' | 'done')}
                    >
                      {row.status.replace('_', ' ')}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    {row.priority === 'critical' ? (
                      <Badge variant="solid">critical</Badge>
                    ) : (
                      <Badge
                        variant="outline"
                        className={priorityBadgeClass(row.priority as 'high' | 'medium')}
                      >
                        {row.priority}
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell className="tabular-nums">{row.points}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </Section>

      <Section
        title="Overlays and elevation"
        intent="Three shadow tokens, warm in light and near-black in dark. Anything that floats gets exactly one of them; there is no fourth."
      >
        <Row label="Shadows">
          <span className="flex h-16 w-24 items-center justify-center rounded-md border border-border bg-surface text-2xs shadow-xs">
            xs
          </span>
          <span className="flex h-16 w-24 items-center justify-center rounded-md border border-border bg-surface text-2xs shadow-card">
            card
          </span>
          <span className="flex h-16 w-24 items-center justify-center rounded-md border border-border bg-surface text-2xs shadow-pop">
            pop
          </span>
          <span className="flex h-16 w-24 items-center justify-center rounded-md border border-border bg-surface text-2xs shadow-overlay">
            overlay
          </span>
        </Row>
        <Row label="Overlays">
          <Dialog>
            <DialogTrigger asChild>
              <Button variant="outline">Open dialog</Button>
            </DialogTrigger>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>Delete sprint 12?</DialogTitle>
                <DialogDescription>
                  The sprint file is removed from the working tree. Items keep their history.
                </DialogDescription>
              </DialogHeader>
              <DialogFooter>
                <Button variant="ghost">Cancel</Button>
                <Button variant="destructive">Delete</Button>
              </DialogFooter>
            </DialogContent>
          </Dialog>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="outline">Hover for a tooltip</Button>
            </TooltipTrigger>
            <TooltipContent>Companion 0.4.0 at http://127.0.0.1:7317</TooltipContent>
          </Tooltip>
          <Button
            variant="outline"
            onClick={() => {
              toast({ title: 'Saved', description: 'ACME-US-0042 written to disk.' });
            }}
          >
            Show a toast
          </Button>
        </Row>
      </Section>

      <Section
        title="Icons"
        intent="Lucide, 1.5px stroke, at three sizes only: 14px inside a chip or a dense row, 16px next to text, 20px for a lone affordance. An icon is always accompanied by a label or an accessible name."
      >
        <Row label="Sizes">
          <GitBranch aria-hidden="true" className="h-3.5 w-3.5" />
          <GitBranch aria-hidden="true" className="h-4 w-4" />
          <GitBranch aria-hidden="true" className="h-5 w-5" />
        </Row>
        <Row label="In context">
          <span className="inline-flex items-center gap-1.5 text-sm text-muted-foreground">
            <GitBranch aria-hidden="true" className="h-4 w-4" />
            feat/team-workspaces
          </span>
          <Badge variant="outline" className={cn(statusBadgeClass('done'))}>
            <Lock aria-hidden="true" />
            protected
          </Badge>
        </Row>
      </Section>
    </div>
  );
}

export function Styleguide() {
  return (
    <TooltipProvider delayDuration={200}>
      <ToastProvider>
        <StyleguideBody />
      </ToastProvider>
    </TooltipProvider>
  );
}
