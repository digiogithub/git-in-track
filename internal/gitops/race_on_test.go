//go:build race

package gitops

// raceEnabled reports whether the race detector is compiled in. See the
// !race build of this file for why the timing budget depends on it.
const raceEnabled = true
