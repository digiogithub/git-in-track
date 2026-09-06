//go:build !race

package gitops

// raceEnabled reports whether the race detector is compiled in. The history
// walk's timing budget is multiplied when it is: the detector instruments every
// memory access, so a walk that takes milliseconds here takes seconds under
// `go test -race` on a shared CI runner.
const raceEnabled = false
