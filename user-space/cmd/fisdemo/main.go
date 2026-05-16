package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

func main() {
	mode  := flag.String("mode", "cpu", "cpu|mem|crash|zombie|orphan|parent-nowait|sigkill|sigterm|sigsegv|sigabrt")
	memMB := flag.Int("mem-mb", 200, "memory to allocate in MB (mem mode only)")
	// Internal flag used when this binary re-execs itself as a child process.
	child := flag.String("child", "", "internal: child behaviour to run")
	flag.Parse()

	// ── child re-exec entry point ─────────────────────────────────────────────
	// When the binary is invoked with -child=<behaviour> it runs that behaviour
	// and exits.  This avoids any dependency on fork(2) which is unreliable
	// inside Go's runtime (goroutine scheduler + signal handling).
	if *child != "" {
		runChild(*child)
		return
	}

	// ── parent entry point ────────────────────────────────────────────────────
	switch *mode {
	case "cpu":
		runCPU()
	case "mem":
		runMem(*memMB)
	case "crash":
		runCrash()
	case "zombie":
		runZombie()
	case "orphan":
		runOrphan()
	case "parent-nowait":
		runParentNoWait()
	case "sigkill":
		runSigkill()
	case "sigterm":
		runSigterm()
	case "sigsegv":
		runSigsegv()
	case "sigabrt":
		runSigabrt()
	default:
		fmt.Fprintf(os.Stderr, "unknown mode: %s\n", *mode)
		os.Exit(1)
	}
}

// ── child behaviours ──────────────────────────────────────────────────────────

func runChild(behaviour string) {
	switch behaviour {
	case "sleep-forever":
		// Used by zombie (child exits immediately after this never runs),
		// orphan, parent-nowait, sigkill, sigterm.
		for {
			time.Sleep(10 * time.Second)
		}

	case "sleep-then-exit":
		// Used by parent-nowait child: sleep briefly then exit normally.
		time.Sleep(2 * time.Second)
		os.Exit(0)

	case "sigsegv":
		// Dereference a nil pointer to raise SIGSEGV.
		time.Sleep(200 * time.Millisecond)
		var p *int
		// Use unsafe to defeat the compiler's nil-pointer optimisation.
		_ = *(*int)(unsafe.Pointer(uintptr(unsafe.Pointer(p))))
		os.Exit(0) // unreachable

	case "sigabrt":
		// Send SIGABRT to self.
		time.Sleep(200 * time.Millisecond)
		p, _ := os.FindProcess(os.Getpid())
		_ = p.Signal(syscall.SIGABRT)
		time.Sleep(1 * time.Second) // wait for signal delivery
		os.Exit(0)                  // unreachable

	default:
		fmt.Fprintf(os.Stderr, "unknown child behaviour: %s\n", behaviour)
		os.Exit(1)
	}
}

// ── helper: spawn a child re-exec of this binary ─────────────────────────────

// spawnChild starts a copy of this binary with -child=<behaviour>.
// It returns the *os.Process of the child.
// If wait is true the child's stdin/stdout/stderr are inherited and the
// function blocks until the child exits (used for fire-and-forget demos).
func spawnChild(behaviour string, inheritIO bool) *os.Process {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "executable: %v\n", err)
		os.Exit(1)
	}
	cmd := exec.Command(self, "-child="+behaviour)
	if inheritIO {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	// Put child in its own process group so signals sent to the parent
	// terminal don't automatically propagate to it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start child: %v\n", err)
		os.Exit(1)
	}
	return cmd.Process
}

// ── original modes ─────────────────────────────────────────────────────────

func runCPU() {
	fmt.Printf("[cpu] PID %d burning CPU\n", os.Getpid())
	for {
		busyFor(900 * time.Millisecond)
		time.Sleep(300 * time.Millisecond)
	}
}

func runMem(memMB int) {
	if memMB <= 0 {
		memMB = 100
	}
	fmt.Printf("[mem] PID %d allocating %d MB\n", os.Getpid(), memMB)
	buf := make([][]byte, 0, memMB)
	for i := 0; i < memMB; i++ {
		buf = append(buf, make([]byte, 1024*1024))
		time.Sleep(10 * time.Millisecond)
	}
	_ = buf
	for {
		runtime.KeepAlive(buf)
		time.Sleep(1 * time.Second)
	}
}

func runCrash() {
	fmt.Printf("[crash] PID %d will exit(2) in 2s\n", os.Getpid())
	time.Sleep(2 * time.Second)
	os.Exit(2)
}

// ── lifecycle demo modes ───────────────────────────────────────────────────

// runZombie: spawn a child that exits immediately; parent never reaps it.
// The child stays as a zombie (state Z in /proc/<pid>/stat) until this
// process exits.
func runZombie() {
	child := spawnChild("sleep-forever", false)
	childPID := child.Pid
	fmt.Printf("[zombie] parent=%d  child=%d\n", os.Getpid(), childPID)

	// Kill the child immediately so it exits and becomes a zombie.
	// We do NOT call Wait, so the kernel keeps the zombie entry in the
	// process table.
	time.Sleep(300 * time.Millisecond)
	_ = child.Signal(syscall.SIGKILL)

	fmt.Printf("[zombie] child %d killed – now zombie (parent holding without wait)\n", childPID)
	fmt.Printf("[zombie] check: cat /proc/%s/status | grep State\n", strconv.Itoa(childPID))

	// Parent loops forever without calling wait() – zombie persists.
	for {
		time.Sleep(10 * time.Second)
	}
}

// runOrphan: spawn a child that sleeps forever; parent exits after 3 s so
// the child is re-parented to PID 1 (init/systemd).
func runOrphan() {
	child := spawnChild("sleep-forever", false)
	fmt.Printf("[orphan] parent=%d  child=%d\n", os.Getpid(), child.Pid)
	fmt.Printf("[orphan] parent exiting in 3s – child %d will be re-parented to PID 1\n", child.Pid)
	time.Sleep(3 * time.Second)
	fmt.Printf("[orphan] parent %d exiting now\n", os.Getpid())
	// Parent exits without waiting – child becomes an orphan.
	os.Exit(0)
}

// runParentNoWait: spawn a child that exits after 2 s; parent also exits
// after 4 s without calling wait().  While the child is between exiting and
// the parent's own exit it is a zombie.  When the parent exits without
// having called wait() the zombie is adopted and reaped by init.
func runParentNoWait() {
	child := spawnChild("sleep-then-exit", false)
	fmt.Printf("[parent-nowait] parent=%d  child=%d\n", os.Getpid(), child.Pid)
	fmt.Printf("[parent-nowait] child will exit in ~2s; parent will exit in ~4s without wait()\n")
	time.Sleep(4 * time.Second)
	fmt.Printf("[parent-nowait] parent %d exiting without reaping child %d\n", os.Getpid(), child.Pid)
	os.Exit(0)
}

// runSigkill: spawn a child that sleeps; parent sends SIGKILL after 3 s.
// Parent calls Wait so the child doesn't linger as zombie – the signal-death
// event fires in procwatch before the reap.
func runSigkill() {
	child := spawnChild("sleep-forever", false)
	fmt.Printf("[sigkill] parent=%d  child=%d\n", os.Getpid(), child.Pid)
	time.Sleep(3 * time.Second)
	fmt.Printf("[sigkill] sending SIGKILL to child %d\n", child.Pid)
	_ = child.Signal(syscall.SIGKILL)
	state, _ := child.Wait()
	fmt.Printf("[sigkill] child %d done  exited=%v  signal=%v\n",
		child.Pid, state.Exited(), state.Sys().(syscall.WaitStatus).Signal())
}

// runSigterm: same as sigkill but sends SIGTERM.
func runSigterm() {
	child := spawnChild("sleep-forever", false)
	fmt.Printf("[sigterm] parent=%d  child=%d\n", os.Getpid(), child.Pid)
	time.Sleep(3 * time.Second)
	fmt.Printf("[sigterm] sending SIGTERM to child %d\n", child.Pid)
	_ = child.Signal(syscall.SIGTERM)
	state, _ := child.Wait()
	fmt.Printf("[sigterm] child %d done  exited=%v  signal=%v\n",
		child.Pid, state.Exited(), state.Sys().(syscall.WaitStatus).Signal())
}

// runSigsegv: spawn a child that nil-dereferences → SIGSEGV.
func runSigsegv() {
	child := spawnChild("sigsegv", true)
	fmt.Printf("[sigsegv] parent=%d  child=%d – child will SIGSEGV\n", os.Getpid(), child.Pid)
	state, _ := child.Wait()
	ws := state.Sys().(syscall.WaitStatus)
	fmt.Printf("[sigsegv] child %d done  signaled=%v  signal=%v\n",
		child.Pid, ws.Signaled(), ws.Signal())
}

// runSigabrt: spawn a child that sends SIGABRT to itself.
func runSigabrt() {
	child := spawnChild("sigabrt", true)
	fmt.Printf("[sigabrt] parent=%d  child=%d – child will SIGABRT\n", os.Getpid(), child.Pid)
	state, _ := child.Wait()
	ws := state.Sys().(syscall.WaitStatus)
	fmt.Printf("[sigabrt] child %d done  signaled=%v  signal=%v\n",
		child.Pid, ws.Signaled(), ws.Signal())
}

// ── utility ───────────────────────────────────────────────────────────────────

func busyFor(d time.Duration) {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
	}
}
