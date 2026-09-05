/**
 * The "make this folder a team repository" form (story GIT-US-0034,
 * docs/04-team-repository.md §3, docs/05-web-app.md §3.1).
 *
 * Boards, sprints, retrospectives and the team knowledge base all live in a
 * team repository, and a team repository is a folder holding a `team.yaml`.
 * Nothing in the product ever wrote one, so the board and retro empty states
 * asked the user for something no surface could produce. This form asks the two
 * things the core needs — the team key and the name — and writes the file.
 */

import { Users } from 'lucide-react';
import { useState } from 'react';

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { TEAM_KEY } from '@/fs';

export type CreateTeamValues = {
  key: string;
  name: string;
  description: string;
};

export type CreateTeamFormProps = {
  /** Disabled while a mount or a creation is in flight. */
  busy: boolean;
  onSubmit: (values: CreateTeamValues) => void;
  /** Optional escape hatch, rendered next to the submit button. */
  onCancel?: () => void;
  /** Label of the submit button; it names what the caller is doing. */
  submitLabel?: string;
};

/** Says why a key is refused, or null when it is fine. */
function keyError(key: string): string | null {
  if (key.trim() === '') return 'A team key is required.';
  if (!TEAM_KEY.test(key.trim())) {
    return 'A key is 2 to 16 characters: an uppercase letter, then uppercase letters, digits or hyphens.';
  }
  return null;
}

export function CreateTeamForm({
  busy,
  onSubmit,
  onCancel,
  submitLabel = 'Create team repository',
}: CreateTeamFormProps): JSX.Element {
  const [key, setKey] = useState('');
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [touched, setTouched] = useState(false);

  const invalidKey = keyError(key);

  return (
    <form
      className="space-y-4"
      onSubmit={(event) => {
        event.preventDefault();
        setTouched(true);
        if (invalidKey) return;
        onSubmit({ key: key.trim(), name: name.trim(), description: description.trim() });
      }}
    >
      <label className="block space-y-1.5 text-sm font-medium" htmlFor="new-team-key">
        Team key
        <Input
          id="new-team-key"
          value={key}
          placeholder="ACME-TEAM"
          aria-invalid={touched && invalidKey !== null}
          onChange={(event) => {
            setKey(event.target.value.toUpperCase());
          }}
        />
        <span className="block text-xs font-normal text-muted-foreground">
          The prefix of every sprint and retro id: <code>ACME-TEAM-S-0001</code>. Hyphens are
          allowed. It cannot be changed later.
        </span>
      </label>
      {touched && invalidKey ? (
        <p role="alert" className="text-sm text-destructive">
          {invalidKey}
        </p>
      ) : null}

      <label className="block space-y-1.5 text-sm font-medium" htmlFor="new-team-name">
        Team name
        <Input
          id="new-team-name"
          value={name}
          placeholder="ACME Delivery Team"
          onChange={(event) => {
            setName(event.target.value);
          }}
        />
        <span className="block text-xs font-normal text-muted-foreground">
          Shown wherever the team is named. It defaults to the key.
        </span>
      </label>

      <label className="block space-y-1.5 text-sm font-medium" htmlFor="new-team-description">
        Description
        <Input
          id="new-team-description"
          value={description}
          placeholder="Squad owning the platform and the website"
          onChange={(event) => {
            setDescription(event.target.value);
          }}
        />
        <span className="block text-xs font-normal text-muted-foreground">
          One optional paragraph.
        </span>
      </label>

      <p className="text-xs text-muted-foreground">
        This writes <code>team.yaml</code>, the <code>.pmngr/</code> folders for boards, sprints
        and retros, and a <code>knowledge/</code> base. The projects the team owns are declared in{' '}
        <code>team.yaml</code> afterwards; a board pulls its cards from them.
      </p>

      <div className="flex flex-wrap items-center gap-2">
        <Button type="submit" disabled={busy}>
          <Users aria-hidden="true" className="h-4 w-4" />
          {busy ? 'Creating…' : submitLabel}
        </Button>
        {onCancel ? (
          <Button type="button" variant="ghost" disabled={busy} onClick={onCancel}>
            Cancel
          </Button>
        ) : null}
      </div>
    </form>
  );
}
