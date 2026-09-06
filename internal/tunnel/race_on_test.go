//go:build race

package tunnel

// raceEnabled reports whether the race detector is compiled in. See the !race
// build of this file for why the live end-to-end test depends on it.
const raceEnabled = true
