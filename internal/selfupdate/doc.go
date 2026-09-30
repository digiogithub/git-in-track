// Package selfupdate finds, downloads and verifies gintrack releases published
// on GitHub. It is native-only: internal/core and wasm/ must never import it.
//
// Release contract (docs/09-ci-cd-and-releases.md, ADR-040):
//
//   - tags are vX.Y.Z (optionally with a -pre suffix);
//   - assets carry the version without the v:
//     gintrack_<X.Y.Z>_linux_<amd64|arm64>.tar.gz and
//     gintrack_<X.Y.Z>_<darwin|windows>_<amd64|arm64>.zip;
//   - checksums.txt is in sha256sum format ("<hex>  <name>");
//   - each archive is flat and holds gintrack (gintrack.exe on Windows),
//     LICENSE and README.md.
//
// The flow is Client.Latest or Client.ByVersion, then SelectAsset, then
// Client.Download. Download streams the archive to a temporary file under a
// size cap, requires the byte count to equal the published asset size,
// requires a matching sha256 entry in checksums.txt, and only then extracts
// the single binary into the destination directory. Verification is
// mandatory and has no opt-out. Nothing in this package touches the installed
// binary; replacing it is the caller's job.
//
// Stable releases only are considered unless the caller asks for
// pre-releases. Rate-limit responses surface as *RateLimitError, which
// mentions GITHUB_TOKEN.
package selfupdate
