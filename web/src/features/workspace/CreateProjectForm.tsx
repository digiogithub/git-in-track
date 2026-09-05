/**
 * The "this repository has no backlog yet" half of the add-repository wizard
 * (story GIT-US-0031, docs/05-web-app.md §3.1).
 *
 * Registering a repository with nothing in it used to end in "mount it anyway",
 * which left the user in an empty workspace with no way forward: no surface of
 * the product ever wrote a `project.yaml`. This form asks the three things the
 * core needs — where the Markdown lives, the project key and the name — and
 * creates the backlog.
 */

import { FolderPlus } from 'lucide-react';
import { useState } from 'react';

import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { normalizeDocsFolder } from '@/fs';

/** The project key grammar of docs/03-data-model.md §3.3. */
const PROJECT_KEY = /^[A-Z][A-Z0-9]{1,9}$/;

export type CreateProjectValues = {
  docsFolder: string;
  key: string;
  name: string;
};

export type CreateProjectFormProps = {
  /** Folders detection found; offered as one-click suggestions. */
  suggestions: string[];
  /** Disabled while a mount or a creation is in flight. */
  busy: boolean;
  onSubmit: (values: CreateProjectValues) => void;
  /** "Mount it anyway": index the folder without creating anything. */
  onSkip: () => void;
};

/** Says why a key is refused, or null when it is fine. */
function keyError(key: string): string | null {
  if (key.trim() === '') return 'A project key is required.';
  if (!PROJECT_KEY.test(key.trim())) {
    return 'A key is 2 to 10 characters: an uppercase letter, then uppercase letters or digits.';
  }
  return null;
}

export function CreateProjectForm({
  suggestions,
  busy,
  onSubmit,
  onSkip,
}: CreateProjectFormProps): JSX.Element {
  const [docsFolder, setDocsFolder] = useState(suggestions[0] ?? 'docs');
  const [key, setKey] = useState('');
  const [name, setName] = useState('');
  const [touched, setTouched] = useState(false);

  const invalidKey = keyError(key);
  const folder = normalizeDocsFolder(docsFolder);

  return (
    <form
      className="space-y-4"
      onSubmit={(event) => {
        event.preventDefault();
        setTouched(true);
        if (invalidKey) return;
        onSubmit({ docsFolder: folder, key: key.trim(), name: name.trim() });
      }}
    >
      <label className="block space-y-1.5 text-sm font-medium" htmlFor="new-project-folder">
        Documentation folder
        <Input
          id="new-project-folder"
          value={docsFolder}
          placeholder="docs"
          onChange={(event) => {
            setDocsFolder(event.target.value);
          }}
        />
        <span className="block text-xs font-normal text-muted-foreground">
          The Markdown lives here; the backlog goes in <code>{folder === '' ? '' : `${folder}/`}
          .pmngr/</code>. Leave it empty for the repository root.
        </span>
      </label>

      {suggestions.length > 0 ? (
        <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
          <span>Suggested:</span>
          {suggestions.map((suggestion) => (
            <Button
              key={suggestion}
              type="button"
              variant="secondary"
              size="sm"
              onClick={() => {
                setDocsFolder(suggestion);
              }}
            >
              {suggestion === '' ? '(repository root)' : suggestion}
            </Button>
          ))}
        </div>
      ) : null}

      <label className="block space-y-1.5 text-sm font-medium" htmlFor="new-project-key">
        Project key
        <Input
          id="new-project-key"
          value={key}
          placeholder="ACME"
          aria-invalid={touched && invalidKey !== null}
          onChange={(event) => {
            setKey(event.target.value.toUpperCase());
          }}
        />
        <span className="block text-xs font-normal text-muted-foreground">
          The prefix of every id: <code>ACME-US-0001</code>. It cannot be changed later.
        </span>
      </label>
      {touched && invalidKey ? (
        <p role="alert" className="text-sm text-destructive">
          {invalidKey}
        </p>
      ) : null}

      <label className="block space-y-1.5 text-sm font-medium" htmlFor="new-project-name">
        Project name
        <Input
          id="new-project-name"
          value={name}
          placeholder="ACME Platform"
          onChange={(event) => {
            setName(event.target.value);
          }}
        />
        <span className="block text-xs font-normal text-muted-foreground">
          Shown in project pickers. It defaults to the key.
        </span>
      </label>

      <div className="flex flex-wrap items-center gap-2">
        <Button type="submit" disabled={busy}>
          <FolderPlus aria-hidden="true" className="h-4 w-4" />
          {busy ? 'Creating…' : 'Create project'}
        </Button>
        <Button type="button" variant="ghost" disabled={busy} onClick={onSkip}>
          Mount it anyway
        </Button>
      </div>
    </form>
  );
}
