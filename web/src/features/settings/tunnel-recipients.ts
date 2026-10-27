import type { TunnelStatus } from '@/api/provider';

/**
 * "2 allowed addresses and 1 domain rule": the allow-list by count, the only
 * form in which it ever reaches the browser.
 */
export function recipientSummary(recipients: TunnelStatus['allowedRecipients']): string {
  const parts: string[] = [];
  if (recipients.emails > 0) {
    parts.push(`${recipients.emails} allowed ${recipients.emails === 1 ? 'address' : 'addresses'}`);
  }
  if (recipients.domains > 0) {
    parts.push(
      `${recipients.domains} ${recipients.domains === 1 ? 'domain rule' : 'domain rules'}`,
    );
  }
  return parts.length === 0 ? 'no allowed recipients' : parts.join(' and ');
}
