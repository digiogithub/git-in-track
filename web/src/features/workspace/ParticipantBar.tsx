import { useState } from 'react';

import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { toHandle, type Identity } from '@/features/workspace/identity';

/**
 * Who is in the room (docs/04 §9.1).
 *
 * A retro served over a tunnel is opened by people the workspace has no account
 * for, so the first thing it asks for is a name. Until one is given the board
 * is read-only: an anonymous vote is a vote nobody can budget, and an anonymous
 * note is a note nobody can ask about. Once given, the handle is remembered in
 * this browser and shown so the visitor can correct it.
 */
export function ParticipantBar({ identity }: { identity: Identity }) {
  const [name, setName] = useState('');
  const [editing, setEditing] = useState(false);

  if (identity.known && !editing) {
    return (
      <p className="text-xs text-muted-foreground">
        Writing as <span className="font-medium text-foreground">{identity.handle}</span>{' '}
        <button
          type="button"
          className="underline"
          onClick={() => {
            setName(identity.handle);
            setEditing(true);
          }}
        >
          change
        </button>
      </p>
    );
  }

  const handle = toHandle(name);

  return (
    <Card>
      <CardContent className="space-y-2 pt-4">
        <p className="text-sm font-medium">Who are you?</p>
        <p className="text-xs text-muted-foreground">
          Your name goes on the notes, votes and comments you add, so the room knows who wrote
          what. It stays in this browser.
        </p>
        <form
          aria-label="Identify yourself"
          className="flex flex-wrap items-center gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            if (handle === '') return;
            identity.identify(name);
            setEditing(false);
          }}
        >
          <Input
            aria-label="Your name"
            placeholder="Ada Lovelace"
            className="w-56"
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
          <Button type="submit" size="sm" disabled={handle === ''}>
            Join the retro
          </Button>
          {handle === '' ? null : (
            <span className="text-xs text-muted-foreground">as {handle}</span>
          )}
          {editing ? (
            <button
              type="button"
              className="text-xs text-muted-foreground underline"
              onClick={() => setEditing(false)}
            >
              Cancel
            </button>
          ) : null}
        </form>
      </CardContent>
    </Card>
  );
}
