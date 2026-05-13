package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/config"
)

type logEntry struct {
	Timestamp string                 `json:"ts"`
	Level     string                 `json:"level"`
	Message   string                 `json:"msg"`
	Fields    map[string]interface{} `json:"fields"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "status":
		statusCmd(os.Args[2:])
	case "events":
		eventsCmd(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("usage: fisctl <status|events> [-config path] [-n count]")
}

func statusCmd(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	configPath := fs.String("config", "configs/fis.json", "path to config file")
	_ = fs.Parse(args)

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config load failed: %v\n", err)
		os.Exit(1)
	}
	data, err := os.ReadFile(cfg.StatusFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "status read failed: %v\n", err)
		os.Exit(1)
	}
	_, _ = os.Stdout.Write(data)
	fmt.Println()
}

func eventsCmd(args []string) {
	fs := flag.NewFlagSet("events", flag.ExitOnError)
	configPath := fs.String("config", "configs/fis.json", "path to config file")
	limit := fs.Int("n", 20, "number of events")
	_ = fs.Parse(args)

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config load failed: %v\n", err)
		os.Exit(1)
	}
	file, err := os.Open(cfg.LogFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "log read failed: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	entries := make([]logEntry, 0, *limit)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var entry logEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Fields == nil {
			continue
		}
		if entry.Fields["kind"] != "fault_event" {
			continue
		}
		entries = append(entries, entry)
		if len(entries) > *limit {
			entries = entries[len(entries)-*limit:]
		}
	}

	for _, entry := range entries {
		fields := entry.Fields
		fmt.Printf("%s target=%s type=%s pid=%d cpu=%.2f mem=%d msg=%s\n",
			entry.Timestamp,
			getString(fields, "target"),
			getString(fields, "type"),
			getInt(fields, "pid"),
			getFloat(fields, "cpu_percent"),
			getInt(fields, "memory_bytes"),
			getString(fields, "message"),
		)
	}
}

func getString(fields map[string]interface{}, key string) string {
	if value, ok := fields[key]; ok {
		if str, ok := value.(string); ok {
			return str
		}
	}
	return ""
}

func getFloat(fields map[string]interface{}, key string) float64 {
	if value, ok := fields[key]; ok {
		if f, ok := value.(float64); ok {
			return f
		}
	}
	return 0
}

func getInt(fields map[string]interface{}, key string) int {
	if value, ok := fields[key]; ok {
		switch v := value.(type) {
		case float64:
			return int(v)
		case int:
			return v
		case int64:
			return int(v)
		}
	}
	return 0
}
