/** Public surface of the browser filesystem layer (docs/05-web-app.md §6). */

export { detectDocsFolders, normalizeDocsFolder, PROJECT_FILE } from './detect-project';
export type { DocsFolderCandidate } from './detect-project';
export {
  FsaVault,
  getDirectoryPicker,
  pickDirectory,
  queryPermission,
  requestPermission,
  toVaultError,
} from './fsa-vault';
export {
  clearHandleRecords,
  getHandleRecord,
  listHandleRecords,
  removeHandleRecord,
  requestPersistentStorage,
  saveHandleRecord,
  supportsHandleStore,
} from './handle-store';
export type { RepoHandleKind, RepoHandleRecord } from './handle-store';
export { MemoryVault } from './memory-vault';
export { diffScans, isTextPath, MAX_TEXT_FILE_BYTES } from './scan';
export {
  detectFolderSupport,
  READ_ONLY_EXPLANATION,
  SUPPORT_MATRIX,
  supportsDirectoryInput,
  supportsFileSystemAccess,
  UNSUPPORTED_EXPLANATION,
} from './support';
export type { FolderAccessLevel, FolderSupport, SupportRow } from './support';
export { VaultError } from './types';
export type {
  DirectoryHandleLike,
  FsPermissionMode,
  FsPermissionState,
  VaultCapabilities,
  VaultFS,
  VaultKind,
} from './types';
export { clearVaultRegistry, forgetVault, getVault, registerVault } from './vault-registry';
export { WebkitDirectoryVault } from './webkit-vault';
