package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/task"
)

var terminalStatuses = map[string]bool{
	"completed":           true,
	"failed":              true,
	"timeout":             true,
	"killed":              true,
	"needs_clarification": true,
	"needs_human":         true,
	"deferred":            true,
}

func NewWaitCmd() *cobra.Command {
	var (
		timeoutFlag string
		jsonOutput  bool
		anyFlag     bool
	)

	cmd := &cobra.Command{
		Use:           "wait <task-id> [task-id...]",
		Short:         "Block until tasks reach terminal status",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadConfig()
			if err != nil {
				return exitError(2, "config error: %v", err)
			}

			store, err := task.NewStore(cfg.TaskDir)
			if err != nil {
				return exitError(1, "opening task store: %v", err)
			}

			timeout, err := time.ParseDuration(timeoutFlag)
			if err != nil {
				timeout = 30 * time.Minute
			}

			pending := map[string]bool{}
			for _, id := range args {
				t, err := store.Get(id)
				if err != nil {
					return exitError(1, "task %s not found: %v", id, err)
				}
				if terminalStatuses[t.Status] {
					if anyFlag {
						return printWaitResult(store, args, jsonOutput)
					}
					continue
				}
				pending[id] = true
			}

			if len(pending) == 0 {
				return printWaitResult(store, args, jsonOutput)
			}

			watcher, err := fsnotify.NewWatcher()
			if err != nil {
				return exitError(1, "creating watcher: %v", err)
			}
			defer watcher.Close()

			for id := range pending {
				path := filepath.Join(cfg.TaskDir, id+".yaml")
				if err := watcher.Add(path); err != nil {
					return exitError(1, "watching %s: %v", id, err)
				}
			}

			deadline := time.After(timeout)

			for len(pending) > 0 {
				select {
				case event, ok := <-watcher.Events:
					if !ok {
						return exitError(1, "watcher closed")
					}
					if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
						continue
					}
					id := filepath.Base(event.Name)
					id = id[:len(id)-5] // strip .yaml
					if !pending[id] {
						continue
					}
					t, err := store.Get(id)
					if err != nil {
						continue
					}
					if terminalStatuses[t.Status] {
						delete(pending, id)
						fmt.Fprintf(os.Stderr, "%s: %s\n", id, t.Status)
						if anyFlag {
							return printWaitResult(store, args, jsonOutput)
						}
					}

				case err, ok := <-watcher.Errors:
					if !ok {
						return exitError(1, "watcher closed")
					}
					fmt.Fprintf(os.Stderr, "watch error: %v\n", err)

				case <-deadline:
					return exitError(124, "wait timed out after %s, %d tasks still pending", timeoutFlag, len(pending))
				}
			}

			return printWaitResult(store, args, jsonOutput)
		},
	}

	cmd.Flags().StringVar(&timeoutFlag, "timeout", "30m", "max wait time")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output final task records as JSON")
	cmd.Flags().BoolVar(&anyFlag, "any", false, "return when any task finishes")
	return cmd
}

func printWaitResult(store *task.Store, ids []string, jsonOut bool) error {
	if !jsonOut {
		return nil
	}

	var tasks []*task.Task
	for _, id := range ids {
		t, err := store.Get(id)
		if err != nil {
			continue
		}
		tasks = append(tasks, t)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(tasks)
}
