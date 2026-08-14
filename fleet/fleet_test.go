package fleet

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// server3Probe is what the probe script actually printed on server3 on the day
// this package was written. Keeping the real output means a change to the
// script is caught by a test rather than by a batch that fails at page one.
const server3Probe = `host=server3
cores=8
load_x100=82
mem_total_mb=15997
mem_free_mb=13217
disk_free_mb=381992
tool=/root/chatgpt-tool/.venv/bin/chatgpt-tool
serve=1
xvfb=/usr/bin/xvfb-run
rsync=/usr/bin/rsync
screen=/usr/bin/screen
`

// server1 is the interesting one. It has the tool, it serves, and it has 553 MB
// free, which is not enough to run a browser.
const server1Probe = `host=server1
cores=4
mem_total_mb=7947
mem_free_mb=553
disk_free_mb=41231
tool=/home/tam/chatgpt-tool/.venv/bin/chatgpt-tool
serve=1
xvfb=/usr/bin/xvfb-run
rsync=/usr/bin/rsync
screen=
`

type fakeRunner struct {
	mu      sync.Mutex
	out     map[string]string
	err     map[string]error
	command string
}

func (f *fakeRunner) Run(_ context.Context, host, command string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.command = command
	if err := f.err[host]; err != nil {
		return "", err
	}
	return f.out[host], nil
}

func TestProbeReadsRealOutput(t *testing.T) {
	runner := &fakeRunner{out: map[string]string{"server3": server3Probe}}
	facts := Probe(context.Background(), runner, Target{Name: "server3", Host: "server3", Port: 8077})

	if facts.Err != "" {
		t.Fatalf("Err = %q", facts.Err)
	}
	if facts.Hostname != "server3" || facts.Cores != 8 || facts.MemFreeMB != 13217 {
		t.Errorf("facts = %+v", facts)
	}
	// The load comes back as hundredths so the probe can stay integers all the
	// way through. 0.82 of eight cores is a quiet box.
	if facts.LoadX100 != 82 {
		t.Errorf("LoadX100 = %d, want 82", facts.LoadX100)
	}
	if facts.Tool != "/root/chatgpt-tool/.venv/bin/chatgpt-tool" {
		t.Errorf("Tool = %q", facts.Tool)
	}
	if !facts.Serving || !facts.Xvfb || !facts.Rsync || !facts.Screen {
		t.Errorf("serving=%t xvfb=%t rsync=%t screen=%t",
			facts.Serving, facts.Xvfb, facts.Rsync, facts.Screen)
	}
	// The port is substituted, not hard coded, because a second fleet on
	// another port is a thing somebody will want.
	if !strings.Contains(runner.command, "127.0.0.1:8077") {
		t.Errorf("script did not carry the port: %s", runner.command)
	}
}

// A host with the tool installed and no memory to run it is worse than a host
// with nothing, because it looks fine until the OOM reaper arrives.
func TestServer1CannotOCR(t *testing.T) {
	runner := &fakeRunner{out: map[string]string{"server1": server1Probe}}
	facts := Probe(context.Background(), runner, Target{Name: "server1", Host: "server1", Port: 8077})

	ok, why := facts.CanOCR()
	if ok {
		t.Fatal("server1 with 553 MB free was called capable")
	}
	if !strings.Contains(why, "553") {
		t.Errorf("reason = %q, want the measured free memory in it", why)
	}
	if got := facts.Lanes(); got != 0 {
		t.Errorf("Lanes = %d, want 0", got)
	}
}

// A full disk is the same shape of problem as no memory, and it went unnoticed
// for longer: the probe read the number and only printed it, so server2 kept
// its lane with nothing left to write to and every chunk sent to it came back
// "No space left on device".
func TestAFullDiskIsRefused(t *testing.T) {
	facts := Facts{Cores: 6, LoadX100: 231, MemFreeMB: 10898, DiskFreeMB: 0,
		Tool: "t", Xvfb: true, Rsync: true}

	ok, why := facts.CanOCR()
	if ok {
		t.Fatal("a host with a full disk was called capable")
	}
	if !strings.Contains(why, "disk") {
		t.Errorf("reason = %q, want it to say which resource ran out", why)
	}
	if got := facts.Lanes(); got != 0 {
		t.Errorf("Lanes = %d, want 0", got)
	}
}

func TestLanes(t *testing.T) {
	for _, c := range []struct {
		name  string
		facts Facts
		want  int
	}{
		// The CPU is the binding constraint on an idle server3: eight cores
		// carry four lanes and 13217 MB would carry eight, so four wins.
		{"server3 idle", Facts{Cores: 8, MemFreeMB: 13217, DiskFreeMB: 12024, Tool: "t", Xvfb: true, Rsync: true}, 4},
		{"two cores plenty of ram", Facts{Cores: 2, MemFreeMB: 16000, DiskFreeMB: 12024, Tool: "t", Xvfb: true, Rsync: true}, 1},
		{"just enough ram", Facts{Cores: 8, MemFreeMB: 1600, DiskFreeMB: 12024, Tool: "t", Xvfb: true, Rsync: true}, 1},
		{"no xvfb", Facts{Cores: 8, MemFreeMB: 16000, DiskFreeMB: 12024, Tool: "t", Rsync: true}, 0},

		// The evening this was written server3 sat at 8.27 across its eight
		// cores running another tenant's work, and it still read 98 pages in
		// that state. One lane, not the four the core count would promise and
		// not the nothing the free cores would.
		{"server3 as it really was", Facts{Cores: 8, LoadX100: 827, MemFreeMB: 13217, DiskFreeMB: 12024, Tool: "t", Xvfb: true, Rsync: true}, 1},
		// server1 the same morning: four cores at 39, a Kubernetes control
		// plane and a registry belonging to somebody else. That is not a slow
		// box, it is a stuck one, and it gets nothing.
		{"a box that is thrashing", Facts{Cores: 4, LoadX100: 3914, MemFreeMB: 13217, DiskFreeMB: 12024, Tool: "t", Xvfb: true, Rsync: true}, 0},
		// The line between the two, drawn at two runnable things per core.
		{"right on the line", Facts{Cores: 4, LoadX100: 800, MemFreeMB: 13217, DiskFreeMB: 12024, Tool: "t", Xvfb: true, Rsync: true}, 0},
		{"just inside it", Facts{Cores: 4, LoadX100: 799, MemFreeMB: 13217, DiskFreeMB: 12024, Tool: "t", Xvfb: true, Rsync: true}, 1},
		// Half the box spoken for is half the lanes.
		{"half spoken for", Facts{Cores: 8, LoadX100: 400, MemFreeMB: 13217, DiskFreeMB: 12024, Tool: "t", Xvfb: true, Rsync: true}, 2},
		// server2 on the same evening: six cores at 0.55, which rounds up to
		// one spoken for, leaving five and so two lanes.
		{"server2 as it really was", Facts{Cores: 6, LoadX100: 55, MemFreeMB: 7363, DiskFreeMB: 44256, Tool: "t", Xvfb: true, Rsync: true}, 2},
		// server2 a day later, with the disk full. Everything else about the
		// box is fine, which is what made this the expensive one to miss.
		{"server2 with the disk full", Facts{Cores: 6, LoadX100: 231, MemFreeMB: 10898, DiskFreeMB: 0, Tool: "t", Xvfb: true, Rsync: true}, 0},
		// Room for two lanes on disk and four on the CPU is two lanes.
		{"disk is the binding constraint", Facts{Cores: 8, MemFreeMB: 13217, DiskFreeMB: 2400, Tool: "t", Xvfb: true, Rsync: true}, 2},
	} {
		if got := c.facts.Lanes(); got != c.want {
			t.Errorf("%s: Lanes = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestProbeReportsAMissingTool(t *testing.T) {
	runner := &fakeRunner{out: map[string]string{"server2": "host=server2\ncores=2\ntool=\n"}}
	facts := Probe(context.Background(), runner, Target{Name: "server2", Host: "server2", Port: 8077})
	if facts.Err == "" {
		t.Fatal("a host without chatgpt-tool was reported as fine")
	}
	ok, why := facts.CanOCR()
	if ok || !strings.Contains(why, "chatgpt-tool") {
		t.Errorf("CanOCR = %t %q", ok, why)
	}
}

// One unreachable host must not lose the answers from the two that replied.
func TestProbeAllKeepsGoing(t *testing.T) {
	runner := &fakeRunner{
		out: map[string]string{"server1": server1Probe, "server3": server3Probe},
		err: map[string]error{"server2": errors.New("ssh: connect to host server2 port 22: Connection refused")},
	}
	rows := ProbeAll(context.Background(), runner, targets("server1", "server2", "server3"))

	if len(rows) != 3 {
		t.Fatalf("got %d rows", len(rows))
	}
	// Order follows the argument, so a report reads the same way twice.
	if rows[0].Name != "server1" || rows[1].Name != "server2" || rows[2].Name != "server3" {
		t.Errorf("order = %s %s %s", rows[0].Name, rows[1].Name, rows[2].Name)
	}
	if rows[1].Err == "" {
		t.Error("the unreachable host looks fine")
	}
	if rows[2].Cores != 8 {
		t.Errorf("server3 lost its facts: %+v", rows[2])
	}
}

func TestTableNamesTheReason(t *testing.T) {
	runner := &fakeRunner{out: map[string]string{"server1": server1Probe, "server3": server3Probe}}
	rows := ProbeAll(context.Background(), runner, targets("server1", "server3"))
	table := Table(rows)
	if !strings.Contains(table, "553") {
		t.Errorf("the table does not say why server1 cannot OCR:\n%s", table)
	}
	if !strings.Contains(table, "server3") {
		t.Errorf("table:\n%s", table)
	}
}

func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "fleet.json")
	state := State{
		Written: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
		Hosts:   map[string]Facts{"server3": {Name: "server3", Cores: 8, Tool: "/root/x"}},
		Tunnels: []Tunnel{{Host: "server3", Route: "server3", LocalPort: 18773, RemotePort: 8077, PID: 4242}},
	}
	if err := state.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// A file naming hosts and ports is nobody else's business.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("mode = %o, want 600", mode)
	}

	back, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if tool, ok := back.Tool("server3"); !ok || tool != "/root/x" {
		t.Errorf("Tool = %q %t", tool, ok)
	}
	tunnel, ok := back.Find("server3")
	if !ok || tunnel.LocalPort != 18773 || tunnel.PID != 4242 {
		t.Errorf("Find = %+v %t", tunnel, ok)
	}
	if _, ok := back.Find("server2"); ok {
		t.Error("found a tunnel that was never started")
	}
}

// No fleet has been brought up yet is a normal state, not a failure, and every
// command has to survive it.
func TestMissingStateIsNotAnError(t *testing.T) {
	state, err := LoadState(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if len(state.Tunnels) != 0 || state.Hosts == nil {
		t.Errorf("state = %+v, want empty and usable", state)
	}
}

// fakeProcess stands in for ssh.
type fakeProcess struct {
	pid    int
	mu     sync.Mutex
	killed bool
}

func (p *fakeProcess) PID() int { return p.pid }
func (p *fakeProcess) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.killed = true
	return nil
}
func (p *fakeProcess) Wait() error { return nil }
func (p *fakeProcess) dead() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.killed
}

// health is a far end that can be turned on and off from a test.
type health struct {
	mu   sync.Mutex
	up   bool
	asks int
}

func (h *health) check(context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.asks++
	if !h.up {
		return errors.New("connection refused")
	}
	return nil
}

func (h *health) set(up bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.up = up
}

func TestUpWaitsForHealth(t *testing.T) {
	far := &health{}
	started := make(chan *fakeProcess, 1)
	supervisor := &Supervisor{Start: func(Link) (Process, error) {
		process := &fakeProcess{pid: 4242}
		// ssh binds the port and the far end starts answering a moment later.
		far.set(true)
		started <- process
		return process, nil
	}}

	tunnels, err := supervisor.Up(context.Background(), []Link{
		{Route: "server3", Host: "server3", LocalPort: 18773, RemotePort: 8077, Check: far.check},
	})
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if len(tunnels) != 1 || tunnels[0].PID != 4242 || tunnels[0].LocalPort != 18773 {
		t.Fatalf("tunnels = %+v", tunnels)
	}
	if len(started) != 1 {
		t.Error("no ssh was started")
	}
}

// Running fleet up twice must not start a second ssh on a port that already has
// one, because the second fails and the failure reads like a broken host.
func TestUpAdoptsAPortThatAlreadyAnswers(t *testing.T) {
	far := &health{up: true}
	var starts int
	supervisor := &Supervisor{Start: func(Link) (Process, error) {
		starts++
		return &fakeProcess{pid: 1}, nil
	}}
	tunnels, err := supervisor.Up(context.Background(), []Link{
		{Route: "server3", Host: "server3", LocalPort: 18773, RemotePort: 8077, Check: far.check},
	})
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if starts != 0 {
		t.Errorf("started %d ssh processes on a port that was already serving", starts)
	}
	if len(tunnels) != 1 || tunnels[0].PID != 0 {
		t.Errorf("tunnels = %+v, want the adopted one with no pid of ours", tunnels)
	}
}

// A tunnel that comes up and never answers is not a tunnel. Leaving the ssh
// running would hold the port and make every later attempt adopt a dead
// forward.
func TestUpKillsATunnelThatNeverAnswers(t *testing.T) {
	far := &health{}
	process := &fakeProcess{pid: 7}
	supervisor := &Supervisor{Start: func(Link) (Process, error) { return process, nil }}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := supervisor.Up(ctx, []Link{
		{Route: "server2", Host: "server2", LocalPort: 18772, RemotePort: 8077, Check: far.check},
	})
	if err == nil {
		t.Fatal("a tunnel that never answered was reported up")
	}
	if !process.dead() {
		t.Error("the ssh was left running")
	}
}

// server2 has been down since before any of this was written. A fleet is what
// answers, not what was configured.
func TestUpReturnsThePartialFleet(t *testing.T) {
	good := &health{up: true}
	bad := &health{}
	supervisor := &Supervisor{
		Logf:  func(string, ...any) {},
		Start: func(Link) (Process, error) { return &fakeProcess{pid: 9}, nil },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	tunnels, err := supervisor.Up(ctx, []Link{
		{Route: "server2", Host: "server2", LocalPort: 18772, Check: bad.check},
		{Route: "server3", Host: "server3", LocalPort: 18773, Check: good.check},
	})
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	if len(tunnels) != 1 || tunnels[0].Route != "server3" {
		t.Fatalf("tunnels = %+v, want only server3", tunnels)
	}
}

// Everything down is an error, because a caller that gets an empty fleet and no
// error will go on to run an OCR batch against nothing.
func TestUpFailsWhenNothingComesUp(t *testing.T) {
	bad := &health{}
	supervisor := &Supervisor{Start: func(Link) (Process, error) { return &fakeProcess{pid: 9}, nil }}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := supervisor.Up(ctx, []Link{{Route: "server2", Check: bad.check}}); err == nil {
		t.Fatal("an empty fleet was reported as success")
	}
}

// One failed check is a blip. Two in a row is a tunnel to restart.
func TestWatchRestartsAfterTwoFailures(t *testing.T) {
	far := &health{up: true}
	starts := make(chan int, 8)
	var count int
	supervisor := &Supervisor{
		Logf: func(string, ...any) {},
		Start: func(Link) (Process, error) {
			count++
			starts <- count
			far.set(true) // the restart works
			return &fakeProcess{pid: 100 + count}, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	link := Link{Route: "server3", Host: "server3", LocalPort: 18773, Check: far.check}
	done := make(chan struct{})
	go func() { supervisor.Watch(ctx, []Link{link}, 5*time.Millisecond); close(done) }()

	far.set(false)
	select {
	case <-starts:
	case <-time.After(2 * time.Second):
		t.Fatal("a tunnel that failed twice was never restarted")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Watch did not stop when the context was cancelled")
	}
}

func TestWatchStopsOnCancel(t *testing.T) {
	far := &health{up: true}
	supervisor := &Supervisor{Start: func(Link) (Process, error) { return &fakeProcess{}, nil }}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		supervisor.Watch(ctx, []Link{{Route: "server3", Check: far.check}}, time.Millisecond)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Watch outlived its context")
	}
}

func TestDownKillsWhatItStarted(t *testing.T) {
	far := &health{}
	process := &fakeProcess{pid: 55}
	supervisor := &Supervisor{Start: func(Link) (Process, error) {
		far.set(true)
		return process, nil
	}}
	if _, err := supervisor.Up(context.Background(), []Link{{Route: "server3", Check: far.check}}); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if errs := supervisor.Down(nil); len(errs) != 0 {
		t.Fatalf("Down: %v", errs)
	}
	if !process.dead() {
		t.Error("Down left the ssh running")
	}
	// Twice is what happens when somebody runs fleet down after a crash.
	if errs := supervisor.Down(nil); len(errs) != 0 {
		t.Errorf("second Down: %v", errs)
	}
}

// A pid from a state file written before a reboot belongs to nothing, or worse,
// to something else. Down must not report that as a failure and must not
// pretend a stale pid is alive.
func TestStalePidsAreNotAlive(t *testing.T) {
	if Alive(0) || Alive(-1) {
		t.Error("a zero pid was called alive")
	}
	if !Alive(os.Getpid()) {
		t.Error("this very process was called dead")
	}
	supervisor := &Supervisor{}
	if errs := supervisor.Down([]Tunnel{{Route: "server3", PID: 0}}); len(errs) != 0 {
		t.Errorf("Down on a tunnel with no pid: %v", errs)
	}
}

// A tunnel must not share a multiplexed connection. This was found by running
// it: server3 is configured with ControlMaster auto and ControlPersist 10m, the
// forward was handed to a master left behind by fleet probe, the pid written to
// the state file belonged to a client that had already exited, and fleet down
// would have killed nothing while the forward stayed up.
func TestTunnelGetsItsOwnConnection(t *testing.T) {
	args := strings.Join(tunnelArgs(Link{Route: "server3", Host: "server3", LocalPort: 18773, RemotePort: 8077}), " ")
	for _, want := range []string{
		"-L 18773:127.0.0.1:8077",
		"ControlMaster=no",
		"ControlPath=none",
		"ExitOnForwardFailure=yes",
		"ServerAliveInterval=20",
		"BatchMode=yes",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("tunnel args missing %q: %s", want, args)
		}
	}
	// ExitOnForwardFailure is what turns a port that is already taken into an
	// ssh that exits, instead of a connection with no forward that looks fine.
	if !strings.HasSuffix(args, " server3") {
		t.Errorf("the host is not last: %s", args)
	}
}

func TestForward(t *testing.T) {
	link := Link{LocalPort: 18773, RemotePort: 8077}
	if got := link.Forward(); got != "18773:127.0.0.1:8077" {
		t.Errorf("Forward = %q", got)
	}
}

// SSH always sets BatchMode, because a command that stops for a passphrase in a
// run started by cron hangs until somebody notices days later.
func TestSSHArgs(t *testing.T) {
	client := SSH{Options: []string{"StrictHostKeyChecking=accept-new"}}
	args := client.args("server3", "echo hi")
	joined := strings.Join(args, " ")
	for _, want := range []string{"BatchMode=yes", "ConnectTimeout=10", "StrictHostKeyChecking=accept-new", "server3", "echo hi"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %v missing %q", args, want)
		}
	}
	// The host comes before the command, or ssh reads the command as the host.
	if index := indexOf(args, "server3"); index < 0 || args[len(args)-1] != "echo hi" {
		t.Errorf("args in the wrong order: %v", args)
	}
}

// targets is the ssh destination and the route name being the same string,
// which is the ordinary case on this fleet.
func targets(names ...string) []Target {
	out := make([]Target, 0, len(names))
	for _, name := range names {
		out = append(out, Target{Name: name, Host: name, Port: 8077})
	}
	return out
}

func indexOf(values []string, want string) int {
	for index, value := range values {
		if value == want {
			return index
		}
	}
	return -1
}

func TestSSHRejectsAnEmptyHost(t *testing.T) {
	if _, err := (SSH{}).Run(context.Background(), "  ", "true"); err == nil {
		t.Fatal("an empty host was accepted")
	}
}

// A remote command that fails prints why on standard error, and dropping that
// leaves nothing to debug but an exit status.
func TestSSHRunKeepsStderr(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no shell")
	}
	client := SSH{Binary: "/bin/sh"}
	// -o BatchMode=yes ... is nonsense to sh, which is the point: it fails and
	// says something, and that something has to reach the caller.
	_, err := client.Run(context.Background(), "server3", "true")
	if err == nil {
		t.Skip("the stand in shell accepted ssh flags")
	}
	if !strings.Contains(err.Error(), "server3") {
		t.Errorf("err = %v, want the host named", err)
	}
}

func TestCondense(t *testing.T) {
	long := strings.Repeat("x", 400)
	got := condense("  a\n  b\t c  ")
	if got != "a b c" {
		t.Errorf("condense = %q", got)
	}
	if got := condense(long); len(got) != 203 {
		t.Errorf("condense kept %d characters", len(got))
	}
}

func TestProbeScriptIsOneCommand(t *testing.T) {
	// Every round trip to these boxes costs a second or more, so the script has
	// to stay one invocation. A stray ssh in it would be a silent second trip.
	if strings.Contains(probeScript, "ssh ") {
		t.Error("the probe script shells out to ssh")
	}
	for _, key := range []string{"host=", "cores=", "mem_free_mb=", "tool=", "serve=", "xvfb=", "rsync="} {
		if !strings.Contains(probeScript, `echo "`+key) {
			t.Errorf("the probe script does not report %s", key)
		}
	}
}

func TestFactsJSONKeepsTheError(t *testing.T) {
	runner := &fakeRunner{err: map[string]error{"server2": fmt.Errorf("connection refused")}}
	facts := Probe(context.Background(), runner, Target{Name: "server2", Host: "server2", Port: 8077})
	if facts.Err == "" || facts.Name != "server2" {
		t.Fatalf("facts = %+v", facts)
	}
	if facts.CheckedAt.IsZero() {
		t.Error("a failed probe has no timestamp, so nothing can tell it from a fresh one")
	}
}
