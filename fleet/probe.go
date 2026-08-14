package fleet

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Facts are what one ssh round trip found on a host.
//
// The point of collecting them in one trip is that every one of them decides
// something. Cores and free memory decide how many browser profiles a host can
// run without swapping; the tool path differs per host and has to be discovered
// rather than assumed; serve decides whether the tunnel is worth starting; and
// a host missing rsync or xvfb-run cannot take OCR work at all.
type Facts struct {
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	Cores    int    `json:"cores"`
	// LoadX100 is the one minute load average times a hundred, because these
	// boxes are rented and shared and the cores are not all ours. server3 has
	// eight of them and sat at a load of eight all evening running somebody
	// else's work, which is not a thing a count of cores can tell you.
	LoadX100   int `json:"load_x100"`
	MemTotalMB int `json:"mem_total_mb"`
	MemFreeMB  int `json:"mem_free_mb"`
	DiskFreeMB int `json:"disk_free_mb"`
	// Tool is the absolute path of chatgpt-tool, which is under /home/tam on
	// server1 and /root on server2 and server3.
	Tool string `json:"tool"`
	// Serving reports whether something holds 127.0.0.1:8077.
	Serving bool `json:"serving"`
	Xvfb    bool `json:"xvfb"`
	Rsync   bool `json:"rsync"`
	Screen  bool `json:"screen"`

	CheckedAt time.Time `json:"checked_at"`
	Err       string    `json:"error,omitempty"`
}

// probeScript is one shell command, not ten, because the round trip to these
// boxes costs more than everything it runs. Every line is key=value so a new
// field on the far side cannot shift the meaning of an old one.
const probeScript = `
echo "host=$(hostname)"
echo "cores=$(nproc)"
echo "load_x100=$(awk '{print int($1*100)}' /proc/loadavg)"
echo "mem_total_mb=$(awk '/MemTotal/{print int($2/1024)}' /proc/meminfo)"
echo "mem_free_mb=$(awk '/MemAvailable/{print int($2/1024)}' /proc/meminfo)"
echo "disk_free_mb=$(df -Pm "$HOME" | awk 'NR==2{print $4}')"
echo "tool=$(command -v chatgpt-tool || ls "$HOME"/chatgpt-tool/.venv/bin/chatgpt-tool 2>/dev/null || echo '')"
echo "serve=$(ss -ltn 2>/dev/null | grep -c '127.0.0.1:PORT')"
echo "xvfb=$(command -v xvfb-run || echo '')"
echo "rsync=$(command -v rsync || echo '')"
echo "screen=$(command -v screen || echo '')"
`

// Target is one host to ask.
//
// Name and Host are two different things and conflating them is a bug waiting
// to happen: the route is called server3 in the route file and in every report,
// while the ssh destination is whatever ~/.ssh/config calls it, which is the
// user's business and not this repo's.
type Target struct {
	Name string
	Host string
	Port int
}

// Probe asks one host what it is.
func Probe(ctx context.Context, runner Runner, target Target) Facts {
	facts := Facts{Name: target.Name, CheckedAt: time.Now().UTC()}
	if facts.Name == "" {
		facts.Name = target.Host
	}
	script := strings.ReplaceAll(probeScript, "PORT", strconv.Itoa(target.Port))
	out, err := runner.Run(ctx, target.Host, script)
	if err != nil {
		facts.Err = err.Error()
		return facts
	}
	for line := range strings.SplitSeq(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "host":
			facts.Hostname = value
		case "cores":
			facts.Cores = number(value)
		case "load_x100":
			facts.LoadX100 = number(value)
		case "mem_total_mb":
			facts.MemTotalMB = number(value)
		case "mem_free_mb":
			facts.MemFreeMB = number(value)
		case "disk_free_mb":
			facts.DiskFreeMB = number(value)
		case "tool":
			facts.Tool = value
		case "serve":
			facts.Serving = number(value) > 0
		case "xvfb":
			facts.Xvfb = value != ""
		case "rsync":
			facts.Rsync = value != ""
		case "screen":
			facts.Screen = value != ""
		}
	}
	if facts.Tool == "" {
		facts.Err = "chatgpt-tool is not installed"
	}
	return facts
}

func number(value string) int {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return n
}

// ProbeAll asks every host at once. They are independent boxes and the trip is
// most of the cost. Results come back in the order of the targets, so a report
// reads the same way twice.
func ProbeAll(ctx context.Context, runner Runner, targets []Target) []Facts {
	out := make([]Facts, len(targets))
	var group sync.WaitGroup
	for index, target := range targets {
		group.Go(func() { out[index] = Probe(ctx, runner, target) })
	}
	group.Wait()
	return out
}

// OCRMemoryMB is what one browser profile needs to transcribe a page without
// the box starting to swap. It is a working figure from the fleet as measured,
// not a published number, and fleet bench is what replaces it.
const OCRMemoryMB = 1500

// OCRDiskMB is what one lane needs free on the disk that holds $HOME.
//
// Measured on server3: the working directory under kvant-ocr is 8 MB, and
// the browser profiles it drives run from about 200 MB to 887 MB, 8,581 MB
// across the 29 of them. A lane reuses a profile rather than making one, but
// Chrome grows the cache in it every session, so the figure that matters is
// the room for the largest profile to grow rather than the room for a new one.
// A thousand is that number rounded up.
const OCRDiskMB = 1000

// CanOCR reports whether a host can run chatgpt-tool ocr-batch.
//
// OCR is a headed Chrome under xvfb-run driving the ChatGPT page. A host
// without xvfb-run cannot start it, a host without rsync cannot be given the
// images, and a host with 553 MB free, which is what server1 had on the day
// this was written, will start it and then be killed by the OOM reaper halfway
// through a batch. Reporting that up front is the difference between a host
// marked incapable and a batch that dies at page eleven.
//
// Disk is in here for the same reason, and it was added after the fact. server2
// filled its disk, and because nothing here looked at disk it kept its lane and
// took work: every chunk of the first Vietnamese translation went to it and
// came back "mkdir: cannot create directory 'kvant-ocr/chat': No space left
// on device". The probe had the number in hand the whole time and only printed
// it.
func (f Facts) CanOCR() (bool, string) {
	switch {
	case f.Err != "":
		return false, f.Err
	case f.Tool == "":
		return false, "chatgpt-tool is not installed"
	case !f.Xvfb:
		return false, "no xvfb-run, so a headed Chrome cannot start"
	case !f.Rsync:
		return false, "no rsync, so the page images cannot be sent"
	case f.MemFreeMB < OCRMemoryMB:
		return false, fmt.Sprintf("%d MB free, and one profile needs about %d", f.MemFreeMB, OCRMemoryMB)
	case f.DiskFreeMB < OCRDiskMB:
		return false, fmt.Sprintf("%d MB free on disk, and one lane needs about %d", f.DiskFreeMB, OCRDiskMB)
	}
	return true, ""
}

// CoresPerLane is how much of a machine one lane wants. A lane is a headed
// Chrome drawing chatgpt.com through swiftshader, because none of these boxes
// has a GPU, and software rasterising a page like that is not a single thread.
const CoresPerLane = 2

// Lanes is how many OCR workers this host can carry.
//
// It counts the cores nobody else is using, not the cores the box has. These
// are rented boxes with other work on them: server3 has eight cores and spent
// an evening at a load average of eight serving somebody else, and every page
// sent to it came back with no composer on it, because a Chrome that cannot get
// the CPU to finish rendering looks from here exactly like a Chrome whose
// selectors have gone stale. server2, six cores at a load of half of one,
// rendered the same page in five seconds.
//
// Memory is in here too but it has never been the thing that runs out: a lane
// measured on server2 peaks around 275 MB against the 1500 this reserves. Disk
// is the thing that ran out, on that same box, and it is in here now.
//
// A box with no spare cores still gets one lane as long as it is not thrashing,
// and that is not a fudge, it is what the usage log says. server3 read 98 pages
// at a load between seven and eight on its eight cores. The first version of
// this refused exactly that box in exactly that state, which would have thrown
// away every page the fleet has read. A lane is mostly a Chrome waiting for an
// answer, so a crowded box is slow rather than useless. A thrashing box is
// useless, and that is what ThrashingLoad is for.
func (f Facts) Lanes() int {
	if ok, _ := f.CanOCR(); !ok {
		return 0
	}
	byMemory := f.MemFreeMB / OCRMemoryMB
	// Each lane drives its own profile and each profile grows, so disk divides
	// the same way memory does. CanOCR has already refused anything under one
	// lane's worth, so this only ever takes lanes off a box that is filling up.
	byDisk := f.DiskFreeMB / OCRDiskMB
	// Hundredths the whole way, so half a core of somebody else's work costs
	// half a core and not a whole one.
	freeX100 := f.Cores*100 - f.LoadX100
	byCPU := freeX100 / (CoresPerLane * 100)
	if byCPU < 1 {
		if f.Thrashing() {
			return 0
		}
		byCPU = 1
	}
	return max(1, min(byMemory, byCPU, byDisk))
}

// ThrashingLoadX100 is the load per core, times a hundred, past which a box is
// not slow but stuck. Two runnable things per core is a box with a queue on it;
// server1 sits at eight and a half and cannot finish an ssh login inside thirty
// seconds, let alone rasterise a page.
const ThrashingLoadX100 = 200

// Thrashing says whether the box is past that. A host with no core count behind
// it has not been measured and is given the benefit of the doubt.
func (f Facts) Thrashing() bool {
	if f.Cores <= 0 {
		return false
	}
	return f.LoadX100 >= ThrashingLoadX100*f.Cores
}

// Table renders probe results the way fleet probe prints them.
func Table(rows []Facts) string {
	var out strings.Builder
	fmt.Fprintf(&out, "%-8s  %-12s  %5s  %5s  %10s  %10s  %6s  %5s  %5s  %s\n",
		"host", "hostname", "cores", "load", "free RAM", "free disk", "serve", "ocr", "lanes", "tool")
	for _, row := range rows {
		if row.Err != "" && row.Hostname == "" {
			fmt.Fprintf(&out, "%-8s  %s\n", row.Name, row.Err)
			continue
		}
		ocr, why := row.CanOCR()
		tool := row.Tool
		if !ocr && why != "" {
			tool = tool + "  (" + why + ")"
		}
		// A capable box with no lanes is the confusing row on this table, so it
		// gets told why: somebody else has the machine, and not by a little.
		if ocr && row.Lanes() == 0 {
			tool = tool + "  (thrashing, another tenant has the box)"
		}
		fmt.Fprintf(&out, "%-8s  %-12s  %5d  %5.2f  %7d MB  %7d MB  %6t  %5t  %5d  %s\n",
			row.Name, row.Hostname, row.Cores, float64(row.LoadX100)/100, row.MemFreeMB, row.DiskFreeMB,
			row.Serving, ocr, row.Lanes(), tool)
	}
	return out.String()
}
