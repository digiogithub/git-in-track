/**
 * Session registry of vaults the user picked in this tab.
 *
 * Picking a folder needs a user gesture, so it happens in a click handler in
 * the workspace UI, while mounting happens in the provider. The UI registers
 * the picked `VaultFS` here and passes the returned id as `MountInput.location`
 * (docs/05-web-app.md §4), which keeps the `DataProvider` interface free of
 * browser-specific types.
 */

import type { VaultFS } from './types';

const vaults = new Map<string, VaultFS>();

function randomId(): string {
  const cryptoApi = globalThis.crypto as { randomUUID?: () => string } | undefined;
  if (cryptoApi?.randomUUID) return cryptoApi.randomUUID().slice(0, 8);
  return Math.random().toString(36).slice(2, 10);
}

/** Registers a vault and returns the id to hand to `mountRepo`. */
export function registerVault(vault: VaultFS, id = `repo-${randomId()}`): string {
  vaults.set(id, vault);
  return id;
}

export function getVault(id: string): VaultFS | undefined {
  return vaults.get(id);
}

export function forgetVault(id: string): void {
  vaults.delete(id);
}

/** Test helper: drops every registered vault. */
export function clearVaultRegistry(): void {
  vaults.clear();
}
