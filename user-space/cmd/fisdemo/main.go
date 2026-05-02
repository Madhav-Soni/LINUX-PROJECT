package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"
)

func main() {
	mode := flag.String("mode", "cpu", "cpu|mem|crash")
	memMB := flag.Int("mem-mb", 200, "memory to allocate in MB")
	flag.Parse()

	switch *mode {
	case "cpu":
		runCPU()
	case "mem":
		runMem(*memMB)
	case "crash":
		runCrash()
	default:
		fmt.Fprintf(os.Stderr, "unknown mode: %s\n", *mode)
		os.Exit(1)
	}
}

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
