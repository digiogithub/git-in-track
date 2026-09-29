package supervisor

import (
	"context"
	"net"
	"strconv"
	"time"
)

// Liveness says whether the files of an instance directory describe something
// that is still running.
type Liveness string

// The three answers of [Inspect].
const (
	// LiveAbsent means the directory has no readable state file: nothing ever
	// ran there, or it was cleaned.
	LiveAbsent Liveness = "absent"
	// LiveStale means the state file claims a running instance but the
	// supervising gintrack, or the child it names, is gone: the file outlived a
	// crash.
	LiveStale Liveness = "stale"
	// LiveRunning means the state file is backed by live processes, or records
	// a clean stop.
	LiveRunning Liveness = "running"
)

// Inspect reads state.json of an instance directory and checks it against the
// process table. It is what `gintrack pando status`, `gintrack mcp` and
// `gintrack spec` use to find an instance they did not start: it never spawns,
// signals or writes anything. A stale file is reported with State forced to
// stopped and LastError saying which process is gone, so a caller can print
// the status as it stands without misreporting it as ready.
func Inspect(dir string) (Status, Liveness) {
	st, err := ReadStatus(dir)
	if err != nil {
		return Status{State: StateStopped}, LiveAbsent
	}
	if st.State == StateStopped {
		return st, LiveRunning
	}
	switch {
	case !PIDAlive(st.SupervisorPID):
		st.State = StateStopped
		st.LastError = "the gintrack serve supervising it (pid " + strconv.Itoa(st.SupervisorPID) + ") is gone"
		return st, LiveStale
	case st.State == StateReady && !PIDAlive(st.PID):
		st.State = StateStopped
		st.LastError = "the pando process (pid " + strconv.Itoa(st.PID) + ") is gone"
		return st, LiveStale
	}
	return st, LiveRunning
}

// Healthy reports whether the instance of a state accepts connections on its
// port. It is the cheap health check of a connect-only process; the MCP
// handshake is the client's.
func Healthy(st Status) bool {
	if st.Port <= 0 {
		return false
	}
	d := net.Dialer{Timeout: 500 * time.Millisecond}
	conn, err := d.DialContext(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(st.Port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
