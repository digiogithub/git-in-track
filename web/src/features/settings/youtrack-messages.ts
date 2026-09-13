/**
 * What a YouTrack problem code means for the person reading it (GIT-EP-0011).
 *
 * It lives apart from the card because the field map renders the same failures,
 * and because the point of the table is that the four remote failures never
 * collapse into one message: each names a different thing to change.
 */

import { ProviderError } from '@/api/provider';

export function youtrackMessage(error: unknown): string {
  if (!(error instanceof ProviderError)) {
    return error instanceof Error ? error.message : String(error);
  }
  switch (error.code) {
    case 'youtrack_not_configured':
      return 'This project is not connected to YouTrack yet. Fill in the instance URL and a permanent token, then test the connection.';
    case 'youtrack_unauthorized':
      return 'YouTrack rejected the token. Create a new permanent token in Profile → Account Security → Authentication and paste it here; the token is not this app’s access token.';
    case 'youtrack_forbidden':
      return 'The token works, but the account behind it may not read this project. Give that account at least “Read Project” — and “Read Issue” for importing — or use a token from an account that already has them.';
    case 'youtrack_not_found':
      return 'The instance answered, but this project does not exist there. Check the project short name, and check whether the URL needs the instance context path (for example https://host/youtrack).';
    case 'youtrack_unreachable':
      return 'The instance could not be reached. Check the host name and that this machine can open the URL — a VPN, a typo in the scheme or a firewall will all look like this.';
    default:
      return error.message;
  }
}
