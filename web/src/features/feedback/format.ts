import type { FeedbackNote } from '@/features/feedback/feedback-store';
import { lineLabel } from '@/features/feedback/selection';

/**
 * Renders the notes of a feedback session as the Markdown body of one item
 * comment: every note quotes the text it is about, says which lines of the
 * description that text sits on, and carries the note below the quote.
 */
export function formatFeedbackComment(notes: FeedbackNote[]): string {
  const count = notes.length === 1 ? '1 note' : `${notes.length} notes`;
  const parts = [`**Feedback** · ${count}`];
  notes.forEach((entry, index) => {
    const where = lineLabel(entry.startLine, entry.endLine);
    const heading = `**${index + 1}.** ${where ? `Description, ${where.toLowerCase()}` : 'Description'}`;
    const quote = entry.quote
      .split('\n')
      .map((line) => `> ${line}`)
      .join('\n');
    parts.push('---', heading, quote, entry.note.trim());
  });
  return parts.join('\n\n');
}
