package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/app"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/config"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/detector"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/events"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/eventstream"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/httpapi"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/logger"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/monitor"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/policy"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/recovery"
	"github.com/Madhav-Soni/LINUX-PROJECT/user-space/internal/state"
)

func main() {
	configPath := flag.String("config", "configs/fis.json", "path to config file")
	once := flag.Bool("once", false, "run one poll and exit")
	httpAddr := flag.String("http", "", "http listen address, for example :8090")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config load failed: %v\n", err)
		os.Exit(1)
	}

	log, err := logger.New(cfg.LogFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logger init failed: %v\n", err)
		os.Exit(1)
	}
	defer log.Close()

	eventStore := eventstream.NewStore(200)
	statusStore := &app.StatusStore{}
	actionObserver := recovery.ActionObserver(func(event events.FaultEvent, result recovery.ActionResult) {
		if result.Type == policy.ActionNone {
			return
		}
		action := eventstream.ActionEvent{
			FaultID:       event.ID,
			CorrelationID: event.CorrelationID,
			Target:        result.Target,
			PID:           result.PID,
			Action:        string(result.Type),
			Result:        result.Result,
			Reason:        result.Reason,
			Error:         result.Error,
			NewPID:        result.NewPID,
		}
		eventStore.Publish(eventstream.NewActionEvent(action))
	})

	runtime, err := app.NewRuntime(cfg, log, actionObserver)
	if err != nil {
		log.Error("runtime init failed", map[string]interface{}{"error": err.Error()})
		os.Exit(1)
	}

	det := detector.New()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var server *http.Server
	if *httpAddr != "" {
		demoManager := httpapi.NewDemoManager("fisdemo")
		handler := httpapi.NewHandler(httpapi.Dependencies{
			ConfigPath: *configPath,
			Runtime:    runtime,
			Status:     statusStore,
			Events:     eventStore,
			Demos:      demoManager,
		})
		server = &http.Server{
			Addr:              *httpAddr,
			Handler:           handler,
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("http server failed", map[string]interface{}{"error": err.Error()})
			}
		}()
	}

	runOnce := func() {
		cfgSnapshot, engine, rec := runtime.Snapshot()
		snapshot, err := monitor.ReadSnapshot()
		if err != nil {
			log.Error("snapshot failed", map[string]interface{}{"error": err.Error()})
			return
		}

		matches := engine.MatchProcesses(snapshot.Processes)
		result := det.Detect(snapshot, matches, engine)
		status := buildStatus(snapshot, matches, result.CPUPercent)
		statusStore.Set(status)
		if err := state.Write(cfgSnapshot.StatusFile, status); err != nil {
			log.Error("status write failed", map[string]interface{}{"error": err.Error()})
		}

		for _, event := range result.Events {
			eventStore.Publish(eventstream.NewFaultEvent(event))
			log.Info("fault event", map[string]interface{}{
				"kind":           "fault_event",
				"event_id":       event.ID,
				"correlation_id": event.CorrelationID,
				"target":         event.Target,
				"pid":            event.PID,
				"type":           event.Type,
				"severity":       event.Severity,
				"message":        event.Message,
				"cpu_percent":    event.CPUPercent,
				"memory_bytes":   event.MemoryBytes,
			})

			target := engine.TargetByName(event.Target)
			if target == nil {
				log.Error("missing target for event", map[string]interface{}{"target": event.Target})
				continue
			}
			policyCfg := engine.EffectivePolicy(target)
			plan := engine.ActionForEvent(target, policyCfg, event)
			if err := rec.Execute(event, plan); err != nil {
				log.Error("action failed", map[string]interface{}{"error": err.Error(), "target": event.Target})
			}
		}
	}

	if *once {
		runOnce()
		return
	}

	interval := time.Duration(cfg.PollIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 2 * time.Second
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("shutdown", map[string]interface{}{"reason": "signal"})
			if server != nil {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = server.Shutdown(shutdownCtx)
				cancel()
			}
			runtime.Cleanup()
			return
		case <-timer.C:
			runOnce()
			cfgSnapshot, _, _ := runtime.Snapshot()
			interval = time.Duration(cfgSnapshot.PollIntervalSeconds) * time.Second
			if interval <= 0 {
				interval = 2 * time.Second
			}
			timer.Reset(interval)
		}
	}
}

func buildStatus(snapshot monitor.Snapshot, matches map[int]*policy.Target, cpuPercent map[int]float64) state.Status {
	byTarget := make(map[string][]state.ProcessStatus)
	for _, proc := range snapshot.Processes {
		target := matches[proc.PID]
		if target == nil {
			continue
		}
		status := state.ProcessStatus{
			PID:         proc.PID,
			Name:        proc.Name,
			Cmdline:     proc.Cmdline,
			CPUPercent:  cpuPercent[proc.PID],
			MemoryBytes: proc.MemoryBytes,
		}
		byTarget[target.Config.Name] = append(byTarget[target.Config.Name], status)
	}

	targets := make([]state.TargetStatus, 0, len(byTarget))
	for name, processes := range byTarget {
		targets = append(targets, state.TargetStatus{Name: name, Processes: processes})
	}

	return state.Status{
		Timestamp: snapshot.Timestamp,
		Targets:   targets,
	}
}
