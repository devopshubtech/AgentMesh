package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/shirou/gopsutil/v4/process"
	"google.golang.org/protobuf/types/known/timestamppb"

	agentv1 "github.com/enfec/agentmesh/protocols/gen/go/agent/v1"
)

const (
	resultOutputCap = 256 << 10 // stored in the final result, per stream
	liveOutputCap   = 1 << 20   // streamed live, per stream
	outputChunk     = 32 << 10
)

// emitter sends messages to the control plane.
type emitter interface {
	// live sends best-effort (dropped while disconnected).
	live(env *agentv1.Envelope)
	// result queues a result for guaranteed delivery on (re)connect.
	result(r *agentv1.CommandResult)
	// inventory sends a fresh inventory snapshot.
	sendInventory()
}

// executor runs verified commands.
type executor struct {
	policy Policy
	emit   emitter
	log    *slog.Logger

	mu      sync.Mutex
	running map[string]context.CancelFunc
}

func newExecutor(p Policy, e emitter, log *slog.Logger) *executor {
	return &executor{policy: p, emit: e, log: log, running: map[string]context.CancelFunc{}}
}

// capabilities advertises what this agent will execute.
func (x *executor) capabilities() []string {
	var caps []string
	if x.policy.execAllowed() {
		caps = append(caps, "exec")
		if x.policy.shellAllowed() {
			caps = append(caps, "exec.shell")
		}
	}
	for _, a := range []string{"ping", "process.list", "inventory.refresh"} {
		if x.policy.actionAllowed(a) {
			caps = append(caps, "action."+a)
		}
	}
	return caps
}

// admit applies local policy. It returns a rejection reason or "".
func (x *executor) admit(spec *agentv1.CommandSpec) string {
	switch spec.GetKind() {
	case "exec":
		if !x.policy.execAllowed() {
			return "arbitrary command execution is disabled by local policy"
		}
		if spec.GetShell() && !x.policy.shellAllowed() {
			return "shell execution is disabled by local policy"
		}
		if len(spec.GetArgv()) == 0 || spec.GetArgv()[0] == "" {
			return "empty command"
		}
	case "action":
		if !x.policy.actionAllowed(spec.GetAction()) {
			return "action disabled by local policy"
		}
		switch spec.GetAction() {
		case "ping", "process.list", "inventory.refresh":
		default:
			return "unknown action " + spec.GetAction()
		}
	default:
		return "unknown command kind"
	}
	x.mu.Lock()
	n := len(x.running)
	x.mu.Unlock()
	if n >= x.policy.maxConcurrent() {
		return "too many concurrent commands"
	}
	return ""
}

func (x *executor) cancel(id string) {
	x.mu.Lock()
	c := x.running[id]
	x.mu.Unlock()
	if c != nil {
		c()
	}
}

// run executes spec and emits its result. It is called in its own goroutine.
func (x *executor) run(spec *agentv1.CommandSpec) {
	id := spec.GetCommandId()
	timeout := time.Duration(spec.GetTimeoutS()) * time.Second
	if timeout <= 0 || timeout > time.Hour {
		timeout = time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	canceled := false
	var cmu sync.Mutex
	x.mu.Lock()
	x.running[id] = func() { cmu.Lock(); canceled = true; cmu.Unlock(); cancel() }
	x.mu.Unlock()
	defer func() {
		x.mu.Lock()
		delete(x.running, id)
		x.mu.Unlock()
		cancel()
	}()

	start := time.Now()
	res := &agentv1.CommandResult{CommandId: id, StartedAt: timestamppb.New(start)}
	switch spec.GetKind() {
	case "action":
		x.runAction(ctx, spec, res)
	default:
		x.runExec(ctx, spec, res)
	}
	cmu.Lock()
	wasCanceled := canceled
	cmu.Unlock()
	switch {
	case wasCanceled:
		res.Status = agentv1.CommandStatus_COMMAND_STATUS_CANCELED
		res.Error = "canceled by operator"
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		res.Status = agentv1.CommandStatus_COMMAND_STATUS_TIMED_OUT
		res.Error = fmt.Sprintf("timed out after %s", timeout)
	}
	res.FinishedAt = timestamppb.Now()
	res.DurationMs = uint64(time.Since(start).Milliseconds())
	x.log.Info("command finished", "command_id", id, "status", res.Status.String(), "exit_code", res.ExitCode, "duration_ms", res.DurationMs)
	x.emit.result(res)
}

func (x *executor) runAction(ctx context.Context, spec *agentv1.CommandSpec, res *agentv1.CommandResult) {
	res.Status = agentv1.CommandStatus_COMMAND_STATUS_SUCCEEDED
	res.HasExitCode, res.ExitCode = true, 0
	switch spec.GetAction() {
	case "ping":
		res.Stdout = []byte("pong\n")
	case "inventory.refresh":
		x.emit.sendInventory()
		res.Stdout = []byte("inventory refreshed\n")
	case "process.list":
		out, err := processTable(ctx)
		if err != nil {
			res.Status, res.ExitCode, res.Error = agentv1.CommandStatus_COMMAND_STATUS_FAILED, 1, err.Error()
			return
		}
		res.Stdout, res.Truncated = capBytes(out, resultOutputCap)
	}
}

func capBytes(b []byte, n int) ([]byte, bool) {
	if len(b) > n {
		return b[:n], true
	}
	return b, false
}

func processTable(ctx context.Context) ([]byte, error) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}
	type row struct {
		pid  int32
		name string
		user string
		rss  uint64
		cpu  float64
	}
	rows := make([]row, 0, len(procs))
	for _, p := range procs {
		r := row{pid: p.Pid}
		r.name, _ = p.NameWithContext(ctx)
		r.user, _ = p.UsernameWithContext(ctx)
		if m, err := p.MemoryInfoWithContext(ctx); err == nil && m != nil {
			r.rss = m.RSS
		}
		r.cpu, _ = p.CPUPercentWithContext(ctx)
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].rss > rows[j].rss })
	if len(rows) > 300 {
		rows = rows[:300]
	}
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PID\tNAME\tUSER\tRSS_MB\tCPU%")
	for _, r := range rows {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%.1f\t%.1f\n", r.pid, r.name, r.user, float64(r.rss)/(1<<20), r.cpu)
	}
	_ = tw.Flush()
	fmt.Fprintf(&buf, "\n%d processes total (top %d by memory shown)\n", len(procs), len(rows))
	return buf.Bytes(), nil
}

// streamWriter keeps a capped copy for the final result and streams chunks live.
type streamWriter struct {
	id     string
	stream agentv1.Stream
	emit   emitter
	mu     sync.Mutex
	buf    bytes.Buffer
	trunc  bool
	sent   int
	seq    uint32
}

func (w *streamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if room := resultOutputCap - w.buf.Len(); room > 0 {
		if len(p) > room {
			w.buf.Write(p[:room])
			w.trunc = true
		} else {
			w.buf.Write(p)
		}
	} else if len(p) > 0 {
		w.trunc = true
	}
	for off := 0; off < len(p) && w.sent < liveOutputCap; {
		end := min(off+outputChunk, len(p), off+(liveOutputCap-w.sent))
		chunk := append([]byte(nil), p[off:end]...)
		w.seq++
		w.emit.live(&agentv1.Envelope{Body: &agentv1.Envelope_CommandOutput{CommandOutput: &agentv1.CommandOutput{
			CommandId: w.id, Stream: w.stream, Seq: w.seq, Data: chunk}}})
		w.sent += end - off
		off = end
	}
	return len(p), nil
}

// scrubbedEnv removes the agent's own configuration from child environments.
func scrubbedEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(strings.ToUpper(kv), "AGENTMESH_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func (x *executor) runExec(ctx context.Context, spec *agentv1.CommandSpec, res *agentv1.CommandResult) {
	argv := spec.GetArgv()
	if spec.GetShell() {
		argv = shellArgv(argv[0])
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = scrubbedEnv()
	stdout := &streamWriter{id: spec.GetCommandId(), stream: agentv1.Stream_STREAM_STDOUT, emit: x.emit}
	stderr := &streamWriter{id: spec.GetCommandId(), stream: agentv1.Stream_STREAM_STDERR, emit: x.emit}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	prepareCommand(cmd)
	cmd.WaitDelay = 5 * time.Second

	err := cmd.Run()
	res.Stdout, res.Stderr = stdout.buf.Bytes(), stderr.buf.Bytes()
	res.Truncated = stdout.trunc || stderr.trunc
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		res.Status, res.HasExitCode, res.ExitCode = agentv1.CommandStatus_COMMAND_STATUS_SUCCEEDED, true, 0
	case errors.As(err, &exitErr):
		res.Status = agentv1.CommandStatus_COMMAND_STATUS_FAILED
		if code := exitErr.ExitCode(); code >= 0 {
			res.HasExitCode, res.ExitCode = true, int32(code)
		} else {
			res.Error = exitErr.String()
		}
	default:
		res.Status = agentv1.CommandStatus_COMMAND_STATUS_FAILED
		res.Error = err.Error()
	}
}
