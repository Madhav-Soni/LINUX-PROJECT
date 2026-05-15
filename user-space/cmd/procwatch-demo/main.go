// procwatch-demo: trigger process-lifecycle faults for manual testing.
//
// Modes:
//
//	zombie        – fork child; parent never calls wait(); child becomes Z
//	orphan        – fork child that sleeps; parent exits immediately; child re-parented to init
//	parent-nowait – fork child; parent exits without SIGCHLD/wait(); child left as zombie briefly
//	sigkill       – fork child; parent sends SIGKILL after 2 s; child dies on signal 9
//	sigterm       – fork child; parent sends SIGTERM after 2 s; child dies on signal 15
//	sigsegv       – child immediately causes SIGSEGV (nil-pointer dereference via unsafe)
//	sigabrt       – child calls os.Exit with SIGABRT via syscall.Kill(self, SIGABRT)
//
// Usage:
//
//	./procwatch-demo -mode zombie
//	./procwatch-demo -mode orphan
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

func main() {
	mode := flag.String("mode", "zombie", "zombie|orphan|parent-nowait|sigkill|sigterm|sigsegv|sigabrt")
	// internal: child re-entry
	childOf := flag.Int("child-of", 0, "internal: parent PID (used by re-invocation)")
	flag.Parse()

	// If this process was re-invoked as a child, run the child body.
	if *childOf != 0 {
		runChild(*mode, *childOf)
		return
	}

	switch *mode {
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

// ── zombie ──────────────────────────────────────────────────────────────────

// demoZombie forks a child that exits immediately; the parent never calls
// wait().  The child will stay in state Z until the parent exits (or is killed
// externally).
func demoZombie() {
	fmt.Printf("[zombie] parent PID=%d  – forking child…\n", os.Getpid())
	cmd := spawnSelf("zombie")
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "fork failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[zombie] child PID=%d started; child will exit immediately, parent will NOT reap it\n", cmd.Process.Pid)

	// Give child time to exit and appear as Z in /proc.
	// Parent intentionally never calls cmd.Wait().
	fmt.Println("[zombie] parent sleeping 60 s – watch /proc for state=Z on the child PID")
	time.Sleep(60 * time.Second)
	fmt.Println("[zombie] parent exiting – kernel will reap the zombie now")
}

// ── orphan ───────────────────────────────────────────────────────────────────

// demoOrphan starts a long-running child then exits immediately.  The child
// continues running, re-parented to PID 1 (init/systemd).
func demoOrphan() {
	fmt.Printf("[orphan] parent PID=%d  – forking long-running child…\n", os.Getpid())
	cmd := spawnSelf("orphan")
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "fork failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[orphan] child PID=%d started; parent exiting NOW – child will be adopted by init\n", cmd.Process.Pid)
	// Parent exits without waiting.  The child process continues.
	os.Exit(0)
}

// ── parent-nowait ─────────────────────────────────────────────────────────────

// demoParentNoWait forks a child that exits, then the parent itself exits
// a few seconds later without calling wait().  The kernel will produce a
// short-lived zombie between the child's exit and the parent's exit.
func demoParentNoWait() {
	fmt.Printf("[parent-nowait] parent PID=%d  – forking child…\n", os.Getpid())
	cmd := spawnSelf("parent-nowait")
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "fork failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[parent-nowait] child PID=%d started\n", cmd.Process.Pid)
	fmt.Println("[parent-nowait] parent sleeping 5 s then exiting without wait()")
	time.Sleep(5 * time.Second)
	fmt.Println("[parent-nowait] parent exiting – child's zombie entry will be reaped by init")
	os.Exit(0)
}

// ── signal-kill ───────────────────────────────────────────────────────────────

// demoSignalKill forks a child that sleeps, then the parent sends sig after 2 s.
func demoSignalKill(sig syscall.Signal) {
	sigName := sigStr(sig)
	fmt.Printf("[%s] parent PID=%d – forking sleeping child…\n", sigName, os.Getpid())
	cmd := spawnSelf("sigkill")
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "fork failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("[%s] child PID=%d started; will send %s in 2 s\n", sigName, cmd.Process.Pid, sigName)
	time.Sleep(2 * time.Second)
	fmt.Printf("[%s] sending %s to child %d\n", sigName, sigName, cmd.Process.Pid)
	if err := cmd.Process.Signal(sig); err != nil {
		fmt.Fprintf(os.Stderr, "signal failed: %v\n", err)
	}
	_ = cmd.Wait()
	fmt.Printf("[%s] child reaped; parent exiting\n", sigName)
}

// ── SIGSEGV ───────────────────────────────────────────────────────────────────

// demoSIGSEGV dereferences a nil pointer to trigger SIGSEGV in the current process.
// Run this directly (not as a parent) since the crash is immediate.
func demoSIGSEGV() {
	fmt.Printf("[sigsegv] PID=%d – about to dereference nil pointer…\n", os.Getpid())
	time.Sleep(500 * time.Millisecond)
	var p *int
	_ = *(*int)(unsafe.Pointer(p)) //nolint:all  – intentional SIGSEGV
}

// ── SIGABRT ───────────────────────────────────────────────────────────────────

// demoSIGABRT sends SIGABRT to itself.
func demoSIGABRT() {
	fmt.Printf("[sigabrt] PID=%d – sending SIGABRT to self…\n", os.Getpid())
	time.Sleep(500 * time.Millisecond)
	_ = syscall.Kill(os.Getpid(), syscall.SIGABRT)
	time.Sleep(1 * time.Second) // unreachable; just in case
}

// ── child body ────────────────────────────────────────────────────────────────

// runChild is called when the process is re-invoked with -child-of.
func runChild(mode string, parentPID int) {
	fmt.Printf("[%s] child PID=%d  parent=%d\n", mode, os.Getpid(), parentPID)
	switch mode {
	case "zombie":
		// Exit immediately so the parent's missing-wait creates a zombie.
		fmt.Printf("[zombie] child exiting immediately (will become Z)\n")
		os.Exit(0)
	case "orphan":
		// Sleep a long time so the orphan is visible in /proc.
		fmt.Printf("[orphan] child sleeping 60 s (should be re-parented to init)\n")
		time.Sleep(60 * time.Second)
	case "parent-nowait":
		// Exit after 1 s so the parent (sleeping 5 s) will have an un-reaped child.
		fmt.Printf("[parent-nowait] child sleeping 1 s then exiting\n")
		time.Sleep(1 * time.Second)
		fmt.Printf("[parent-nowait] child exiting\n")
		os.Exit(0)
	case "sigkill", "sigterm":
		// Sleep until killed.
		fmt.Printf("[%s] child sleeping (waiting to be killed)…\n", mode)
		time.Sleep(30 * time.Second)
	default:
		time.Sleep(10 * time.Second)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// spawnSelf returns an *exec.Cmd that re-invokes this binary with the given
// mode and -child-of flag so the child takes the child path in main().
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
