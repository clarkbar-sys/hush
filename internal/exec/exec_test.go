package exec

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// collect runs a spec and returns the events, guarding emit with a mutex since
// Run promises serial delivery but tests shouldn't depend on that for safety.
func collect(ctx context.Context, spec Spec) []Event {
	var mu sync.Mutex
	var evs []Event
	Run(ctx, spec, func(e Event) {
		mu.Lock()
		evs = append(evs, e)
		mu.Unlock()
	})
	return evs
}

func exitEvent(t *testing.T, evs []Event) Event {
	t.Helper()
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind == "exit" {
			return evs[i]
		}
	}
	t.Fatalf("no exit event in %+v", evs)
	return Event{}
}

func output(evs []Event) string {
	var b strings.Builder
	for _, e := range evs {
		if e.Kind == "out" {
			b.WriteString(e.Data)
		}
	}
	return b.String()
}

func TestRunCapturesStdoutAndExitZero(t *testing.T) {
	evs := collect(context.Background(), Spec{Cmd: "echo hello"})
	if len(evs) == 0 || evs[0].Kind != "start" || evs[0].PID == 0 {
		t.Fatalf("first event should be start with a pid: %+v", evs)
	}
	if got := output(evs); !strings.Contains(got, "hello") {
		t.Errorf("output = %q, want it to contain hello", got)
	}
	ex := exitEvent(t, evs)
	if ex.Code != 0 || ex.Signal != "" {
		t.Errorf("exit = %+v, want code 0 no signal", ex)
	}
}

func TestRunReportsNonZeroExit(t *testing.T) {
	ex := exitEvent(t, collect(context.Background(), Spec{Cmd: "exit 3"}))
	if ex.Code != 3 {
		t.Errorf("exit code = %d, want 3", ex.Code)
	}
}

func TestRunTagsStderr(t *testing.T) {
	evs := collect(context.Background(), Spec{Cmd: "echo oops 1>&2"})
	var sawStderr bool
	for _, e := range evs {
		if e.Kind == "out" && e.Stream == "stderr" && strings.Contains(e.Data, "oops") {
			sawStderr = true
		}
	}
	if !sawStderr {
		t.Errorf("expected a stderr-tagged out event, got %+v", evs)
	}
}

func TestRunEmptyCommandErrors(t *testing.T) {
	evs := collect(context.Background(), Spec{Cmd: "   "})
	if len(evs) != 1 || evs[0].Kind != "error" {
		t.Fatalf("want a single error event, got %+v", evs)
	}
}

func TestRunTimeoutKills(t *testing.T) {
	start := time.Now()
	ex := exitEvent(t, collect(context.Background(), Spec{Cmd: "sleep 30", Timeout: 200 * time.Millisecond}))
	if time.Since(start) > 5*time.Second {
		t.Fatalf("run did not stop promptly after the timeout")
	}
	if ex.Signal != "timeout" {
		t.Errorf("signal = %q, want timeout", ex.Signal)
	}
}

func TestRunContextCancelKills(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(150 * time.Millisecond); cancel() }()
	start := time.Now()
	ex := exitEvent(t, collect(ctx, Spec{Cmd: "sleep 30"}))
	if time.Since(start) > 5*time.Second {
		t.Fatalf("run did not stop promptly after cancel")
	}
	if ex.Signal != "canceled" {
		t.Errorf("signal = %q, want canceled", ex.Signal)
	}
}

func TestRunKillsProcessGroup(t *testing.T) {
	// The child backgrounds a grandchild in the same group; killing the group
	// (not just the shell) is what makes a timeout actually stop the work.
	ex := exitEvent(t, collect(context.Background(), Spec{
		Cmd:     "sleep 30 & echo started; wait",
		Timeout: 200 * time.Millisecond,
	}))
	if ex.Signal != "timeout" {
		t.Errorf("signal = %q, want timeout", ex.Signal)
	}
}

func TestRunTruncatesHugeOutput(t *testing.T) {
	// yes floods far past the 1 MiB cap; the run should stop itself via timeout
	// and report truncation rather than buffering unboundedly.
	ex := exitEvent(t, collect(context.Background(), Spec{
		Cmd:     "yes AAAAAAAAAAAAAAAA",
		Timeout: 2 * time.Second,
	}))
	if !ex.Truncated {
		t.Errorf("expected Truncated=true, got %+v", ex)
	}
}
