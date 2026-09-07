# ADR-028 — Retro participants name themselves in the browser, and the discussion is front matter

- **Status:** Accepted
- **Date:** 2026-09-07
- **Phase:** 5 (Retrospectives, `GIT-EP-0006`)
- **Related:** [ADR-005](ADR-005-companion-cli-go-embed.md), [ADR-027](ADR-027-cloudflared-as-a-library-for-quick-tunnels.md)
- **Implements:** `GIT-US-0027` — Run a retrospective

## Context

A retrospective is the one part of this product that several people use *at the
same time*. Since ADR-027 the way they do it is a quick tunnel: the person who
owns the workspace runs `gintrack serve`, shares the tunnel URL, and everybody
else opens the same companion from wherever they are.

That surfaced three gaps in the retro as it was built.

1. **Nobody had a name.** The app attributed every write to the first handle in
   `team.yaml` — a member list that is frequently empty, and that in any case
   does not contain the guest who just joined the call. Every note came from the
   same person, and votes could not be budgeted because there was only ever one
   voter.
2. **There was nothing to vote on.** The ballot is keyed by theme, and themes
   only exist once somebody groups notes. There is no grouping UI, so moving the
   stage to `voting` showed an empty ranked list and no vote button anywhere.
3. **There was nowhere to put what the room said.** The `discussing` stage had
   no writable surface at all; the outcome of a discussion could only be
   recorded as an improvement action, which loses everything that is not an
   action.

The product has no server-side identity and will not grow one: there is no
account system, no session, and the security model is a bearer token plus a
loopback bind (ADR-005). Whatever identity a retro uses has to be compatible
with that.

## Decision

**1. A participant names themselves, in their own browser.**

The handle is chosen once, slugified to the `[a-z0-9][a-z0-9-]{0,31}` shape the
retro's own validation accepts, and kept in `localStorage` under
`gintrack:identity`. It is sent as the `author` of the notes and comments that
browser writes, and as its entry in `votes`. Until a name is given, the retro is
readable but not writable.

This is a **label, not a credential**. It is not checked, not unique, and grants
nothing: anyone who can reach the tunnel can already write, and the token is
what decides that (ADR-005, ADR-027). What the handle buys is the thing a retro
actually needs — the room being able to see who wrote what, and a vote budget
that can be counted per person.

`localStorage` rather than `sessionStorage`, unlike the companion token: a token
that outlives its companion is a hazard, whereas the same laptop being the same
person next sprint is the point.

**2. Voting is per note; a note nobody grouped becomes a theme of its own.**

The file format does not change: `votes` stays keyed by theme id. What changes
is that the first vote on an ungrouped note writes a theme for that note in the
same patch — `id: t-<note id>`, `title` the note's text, `notes: [<note id>]` —
so the room votes on the wall it wrote instead of on a grouping it has to build
first. Grouping duplicates remains a deliberate act, and a note that *was*
grouped is voted on through its theme, so a merge still pools the votes.

The alternative — a second ballot keyed by note id — was rejected: two vote maps
in one file is two things to validate, two ways to spend a budget, and a merge
that silently splits a theme's votes in half.

**3. The discussion is `comments` in front matter, one remark per card.**

A comment carries `id`, exactly one of `note` / `theme`, `author`, `text` and
`created`. Front matter rather than a `## Discussion` body section, for the same
reason a note is one bullet: one comment is one block of consecutive lines, so
two people commenting at the same time touch different lines and git merges both
sides. Sections the tool does not own stay verbatim, and the body remains the
document a human wrote — a whiteboard retro transcribed by hand still round
trips.

**4. The wall refreshes itself.** The companion already watches the team
repository and emits change events for `.pmngr` writes; the retro subscribes to
them and refetches, so a note another participant adds appears without a reload.

## Consequences

- New front-matter key `comments` on retro files, and two new diagnostics:
  `E-RETRO-COMMENT-ID-DUP` and `E-RETRO-COMMENT-TARGET`. Documented in
  docs/04 §9.2 and §9.5 (R-RETRO-6, R-RETRO-7, R-RETRO-8).
- Removing a note removes the comments attached to it, so a remark never dangles
  off a card that is gone.
- An older binary reading a newer file keeps `comments` through `Extra`, as it
  does every other key it does not model.
- The handle is per browser profile, so one person on two machines is two
  handles until they type the same name twice. That is accepted: the cost of
  fixing it is an account system.
- `participants` in front matter stays the facilitator's declared list. A guest
  who names themselves and votes is reported by `W-RETRO-VOTE-NONPARTICIPANT`,
  which is the correct outcome — a warning the facilitator can settle by adding
  them, never a refusal that loses their vote.

## Alternatives considered

- **Accounts, or a handle typed per write.** An account system contradicts
  ADR-002 (no central server) and buys nothing a retro needs. Typing a name on
  every note is friction in the one part of the app where friction stops the
  practice.
- **Deriving the handle from `team.yaml`.** This is what was built, and it is
  what broke: the list is often empty and never contains the guest in the call.
- **A second ballot keyed by note id.** Two vote maps to validate, two ways to
  spend one budget, and a later merge of two notes into a theme silently splits
  their votes. Rejected in favour of promoting an ungrouped note to a theme.
- **A grouping UI first, voting second.** It is the honest reading of §9.1 step
  2, and it is still worth building — but it makes voting depend on a
  facilitator doing clerical work mid-session, which is exactly when nobody has
  the attention for it. Per-note voting works without it and stays correct with
  it.
- **A `## Discussion` body section for comments.** Reads best in a plain editor,
  merges worst: two people appending prose to the same section collide on the
  same lines, and the tool would have to own a section it currently preserves
  verbatim.
