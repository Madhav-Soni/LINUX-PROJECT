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
	// internal flag: set when this binary is re-invoked as a child process
	childOf := flag.Int("child-of", 0, "internal: parent PID (re-invocation)")
	flag.Parse()

	// Child re-entry path (re-invoked by parent for lifecycle demos).
	if *childOf != 0 {
		runChild(*mode, *childOf)
		return
	}

	switch *mode {
	case "cpu":
		runCPU()
	case "mem":
		runMem(*memMB)
	case "crash":
		runCrash()
	case "zombie":
		demoZombie()
	case "orphan":
		demoOrphan()
	case "parent-nowait":
		demoParentNoWait()
	case "sigkill":
		demoSignalKill(syscall.SIGKILL)
	case "sigterm":
		demoSignalKill(syscall.SIGTERM)
	case "sigsegv":
		demoSIGSEGV()
	case "sigabrt":
		demoSIGABRT()
	default:
		fmt.Fprintf(os.Stderr, "unknown mode: %s\n", *mode)
		os.Exit(1)
	}
}

// ── original modes ────────────────────────────────────────────────────────────

func runCPU() {
	for {
		busyFor(900 * time.Millisecond)
		time.Sleep(300 * time.Millisecond)
	}
}

func runMem(memMB int) {
	if memMB <= 0 {
		memMB = 100
	}
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
	time.Sleep(2 * time.Second)
	os.Exit(2)
}

func busyFor(d time.Duration) {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
	}
}

// ── lifecycle demo modes ──────────────────────────────────────────────────────

// demoZombie forks a child that exits immediately; parent never calls wait().
// Child appears as state Z in /proc for up to 60 s.
func demoZombie() {
	fmt.Printf("[zombie] parent PID=%d – forking child\n", os.Getpid())
	cmd := spawnSelf("zombie")
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "fork failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[zombie] child PID=%d started; child exits immediately, parent will NOT reap it\n", cmd.Process.Pid)
	// Parent intentionally never calls cmd.Wait() → zombie.
	fmt.Println("[zombie] parent sleeping 60s; watch /proc for state=Z on child PID")
	time.Sleep(60 * time.Second)
}

// demoOrphan starts a long-running child then exits immediately.
// The child is re-parented to PID 1 (init/systemd) – orphan.
func demoOrphan() {
	fmt.Printf("[orphan] parent PID=%d – forking long-running child\n", os.Getpid())
	cmd := spawnSelf("orphan")
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "fork failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[orphan] child PID=%d; parent exiting NOW → child adopted by init\n", cmd.Process.Pid)
	os.Exit(0)
}

// demoParentNoWait forks a child that exits; parent exits 5 s later without
// calling wait().  Brief zombie window is detectable.
func demoParentNoWait() {
	fmt.Printf("[parent-nowait] parent PID=%d – forking child\n", os.Getpid())
	cmd := spawnSelf("parent-nowait")
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "fork failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[parent-nowait] child PID=%d; parent exits in 5s without wait()\n", cmd.Process.Pid)
	time.Sleep(5 * time.Second)
	fmt.Println("[parent-nowait] parent exiting")
	os.Exit(0)
}

// demoSignalKill forks a sleeping child then sends sig after 2 s.
func demoSignalKill(sig syscall.Signal) {
	name := sigStr(sig)
	fmt.Printf("[%s] parent PID=%d – forking sleeping child\n", name, os.Getpid())
	cmd := spawnSelf("sigkill") // child body: sleep until killed
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "fork failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[%s] child PID=%d; sending %s in 2s\n", name, cmd.Process.Pid, name)
	time.Sleep(2 * time.Second)
	fmt.Printf("[%s] sending %s → child %d\n", name, name, cmd.Process.Pid)
	_ = cmd.Process.Signal(sig)
	_ = cmd.Wait()
	fmt.Printf("[%s] child reaped; parent exiting\n", name)
}

// demoSIGSEGV triggers SIGSEGV by nil-pointer dereference.
func demoSIGSEGV() {
	fmt.Printf("[sigsegv] PID=%d – dereferencing nil pointer\n", os.Getpid())
	time.Sleep(500 * time.Millisecond)
	var p *int
	_ = *(*int)(unsafe.Pointer(p)) //nolint:all – intentional SIGSEGV
}

// demoSIGABRT sends SIGABRT to itself.
func demoSIGABRT() {
	fmt.Printf("[sigabrt] PID=%d – sending SIGABRT to self\n", os.Getpid())
	time.Sleep(500 * time.Millisecond)
	_ = syscall.Kill(os.Getpid(), syscall.SIGABRT)
	time.Sleep(1 * time.Second)
}

// ── child body (re-invocation) ────────────────────────────────────────────────

func runChild(mode string, parentPID int) {
	fmt.Printf("[%s] child PID=%d parent=%d\n", mode, os.Getpid(), parentPID)
	switch mode {
	case "zombie":
		// Exit immediately → parent's missing wait() creates zombie.
		fmt.Printf("[zombie] child exiting immediately (will become Z)\n")
		os.Exit(0)
	case "orphan":
		// Sleep so orphan is visible in /proc.
		fmt.Printf("[orphan] child sleeping 60s (will be re-parented to init)\n")
		time.Sleep(60 * time.Second)
	case "parent-nowait":
		// Exit after 1 s – parent sleeps 5 s, so child is briefly a zombie.
		fmt.Printf("[parent-nowait] child sleeping 1s then exiting\n")
		time.Sleep(1 * time.Second)
		os.Exit(0)
	case "sigkill", "sigterm":
		// Sleep until the parent kills us.
		fmt.Printf("[%s] child sleeping (waiting to be killed)\n", mode)
		time.Sleep(30 * time.Second)
	default:
		time.Sleep(10 * time.Second)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// spawnSelf returns an *exec.Cmd that re-invokes this binary with the given
// mode and -child-of so the child takes the child path in main().
func spawnSelf(mode string) *exec.Cmd {
	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}
	return exec.Command(self,
		"-mode", mode,
		"-child-of", strconv.Itoa(os.Getpid()),
	)
}

func sigStr(sig syscall.Signal) string {
	switch sig {
	case syscall.SIGKILL:
		return "SIGKILL"
	case syscall.SIGTERM:
		return "SIGTERM"
	case syscall.SIGSEGV:
		return "SIGSEGV"
	case syscall.SIGABRT:
		return "SIGABRT"
	default:
		return fmt.Sprintf("SIG(%d)", sig)
	}
}
