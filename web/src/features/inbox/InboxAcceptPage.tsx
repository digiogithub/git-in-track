import { ItemEditorPage } from '@/features/editor/ItemEditorPage';

/**
 * Accepting a submission into the backlog (ADR-033, story GIT-US-0060).
 *
 * There is no second form here, and that is the point of the flow: accepting is
 * the ordinary item editor with a different verb. `ItemEditorPage` in `accept`
 * mode offers the same `FrontMatterForm` and `MarkdownEditor`, defaults the
 * status to the workflow's initial non-triage one, and commits the acceptance
 * itself rather than a plain patch — so a person cannot half-accept an item by
 * editing it and walking away.
 *
 * The route is kept separate because the URL is part of the triage pass: a
 * half-finished queue is a link, and `/p/$project/inbox/$id/accept` is where
 * that link goes.
 */
export function InboxAcceptPage() {
  return <ItemEditorPage mode="accept" />;
}
