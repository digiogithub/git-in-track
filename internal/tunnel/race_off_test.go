//go:build !race

package tunnel

// raceEnabled reports whether the race detector is compiled in. The live
// end-to-end test cannot run under it: cloudflared's own DNS resolver races on
// itself. ingress/origins.(*resolver).peekDial writes r.network and r.address
// with no synchronization, and Go's resolver calls Dial from several
// goroutines at once when it queries more than one nameserver, so any tunnel
// that resolves a name trips the detector inside a dependency we do not own.
const raceEnabled = false
