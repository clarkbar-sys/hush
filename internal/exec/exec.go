// Package exec runs a one-shot command on the host and streams its output —
// the engine behind the Task construct ("a one-shot run of a program —
// ephemeral"). Like package browse it is deliberately unjailed: the command
// runs as the unprivileged "hush" user the agent runs as, and the OS's own
// permissions are the only boundary. There is no allowlist of binaries; whatever
// that user can do in a shell, a Task can do. The security fence is the Unix
// identity, not this code — the same model /browse uses.
package exec

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// DefaultTimeout bounds a run when the caller doesn't ask for one, so a hung
	// command can't pin a process forever.
	DefaultTimeout = 5 * time.Minute
	// MaxTimeout caps what a caller may request.
	MaxTimeout = 60 * time.Minute
	// maxOutput caps captured output. Past this the run keeps going but further
	// output is dropped and Truncated is reported on the exit event, mirroring
	// the way /browse caps a pathological directory listing.
	maxOutput = 1 << 20 // 1 MiB
)

// Event is one frame in a run's lifecycle, delivered to the caller as it
// happens. It is JSON-friendly so an HTTP layer can stream it verbatim.
type Event struct {
	Kind      string `json:"kind"`                // "start" | "out" | "exit" | "error"
	Stream    string `json:"stream,omitempty"`    // "stdout" | "stderr" (kind=out)
	Data      string `json:"data,omitempty"`      // output chunk (out) or message (error)
	PID       int    `json:"pid,omitempty"`       // kind=start
	Code      int    `json:"code,omitempty"`      // kind=exit: process exit code (-1 if signalled)
	Signal    string `json:"signal,omitempty"`    // kind=exit: "timeout", "canceled", or a signal name
	MS        int64  `json:"ms,omitempty"`        // kind=exit: wall-clock duration in ms
	Truncated bool   `json:"truncated,omitempty"` // kind=exit: output cap was hit
}

// Spec describes a run.
type Spec struct {
	Cmd     string        // shell command line, run via `sh -c`
	Timeout time.Duration // 0 => DefaultTimeout; clamped to MaxTimeout
}

// Run executes spec and delivers its lifecycle as Events to emit, in order, from
// the calling goroutine (emit is never called concurrently). It returns once the
// process exits, is killed by the timeout, or ctx is cancelled — whichever comes
// first. The command runs in its own process group so a timeout or a client
// hang-up kills the whole tree, not just the shell.
func Run(ctx context.Context, spec Spec, emit func(Event)) {
	cmdline := strings.TrimSpace(spec.Cmd)
	if cmdline == "" {
		emit(Event{Kind: "error", Data: "empty command"})
		return
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if timeout > MaxTimeout {
		timeout = MaxTimeout
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	c := exec.CommandContext(runCtx, "sh", "-c", cmdline)
	// Own process group so we can signal the whole tree, and a Cancel that kills
	// the group (not just the shell) when the deadline trips or the client hangs
	// up. WaitDelay bounds how long Wait blocks after that on stuck pipes.
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process != nil {
			return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	c.WaitDelay = 2 * time.Second

	stdout, err := c.StdoutPipe()
	if err != nil {
		emit(Event{Kind: "error", Data: err.Error()})
		return
	}
	stderr, err := c.StderrPipe()
	if err != nil {
		emit(Event{Kind: "error", Data: err.Error()})
		return
	}

	start := time.Now()
	if err := c.Start(); err != nil {
		emit(Event{Kind: "error", Data: err.Error()})
		return
	}
	emit(Event{Kind: "start", PID: c.Process.Pid})

	// Two readers feed one channel so emit stays single-threaded and ordering
	// within each stream is preserved (interleaving between the two is
	// best-effort, exactly as a terminal shows it).
	type chunk struct{ stream, data string }
	ch := make(chan chunk, 64)
	var total atomic.Int64
	var truncated atomic.Bool
	var wg sync.WaitGroup
	pump := func(r io.Reader, label string) {
		defer wg.Done()
		buf := make([]byte, 4096)
		br := bufio.NewReader(r)
		for {
			n, err := br.Read(buf)
			if n > 0 {
				if total.Add(int64(n)) > maxOutput {
					truncated.Store(true)
				} else {
					ch <- chunk{label, string(buf[:n])}
				}
			}
			if err != nil {
				return
			}
		}
	}
	wg.Add(2)
	go pump(stdout, "stdout")
	go pump(stderr, "stderr")
	go func() { wg.Wait(); close(ch) }()

	for c := range ch {
		emit(Event{Kind: "out", Stream: c.stream, Data: c.data})
	}

	err = c.Wait()
	ev := Event{Kind: "exit", MS: time.Since(start).Milliseconds(), Truncated: truncated.Load()}
	switch {
	case err == nil:
		ev.Code = 0
	default:
		ev.Code = -1
		if ee, ok := err.(*exec.ExitError); ok {
			ev.Code = ee.ExitCode()
			if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				ev.Signal = ws.Signal().String()
			}
		} else {
			ev.Signal = err.Error()
		}
		// A killed process reports a signal; name *why* we killed it when the
		// context is what tripped, so the UI can say "timeout" vs "canceled".
		switch {
		case runCtx.Err() == context.DeadlineExceeded:
			ev.Signal = "timeout"
		case ctx.Err() != nil:
			ev.Signal = "canceled"
		}
	}
	emit(ev)
}
