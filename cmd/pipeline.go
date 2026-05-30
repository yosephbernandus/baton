package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/yosephbernandus/baton/internal/config"
	"github.com/yosephbernandus/baton/internal/events"
	"github.com/yosephbernandus/baton/internal/gateway"
	gitpkg "github.com/yosephbernandus/baton/internal/git"
	"github.com/yosephbernandus/baton/internal/phase"
	"github.com/yosephbernandus/baton/internal/runner"
	"github.com/yosephbernandus/baton/internal/session"
	"github.com/yosephbernandus/baton/internal/spec"
	"github.com/yosephbernandus/baton/internal/task"
)

func NewPipelineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pipeline",
		Short: "Run a multi-phase pipeline for a task spec",
	}

	cmd.AddCommand(newPipelineRunCmd())
	cmd.AddCommand(newPipelineStatusCmd())
	return cmd
}

func newPipelineRunCmd() *cobra.Command {
	var (
		complexityFlag string
		freshFlag      bool
	)

	cmd := &cobra.Command{
		Use:           "run <spec.yaml>",
		Short:         "Execute a 16-phase pipeline for a task spec",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			specPath := args[0]

			cfg, err := config.LoadConfig()
			if err != nil {
				return exitError(2, "config error: %v", err)
			}

			s, err := spec.Load(specPath)
			if err != nil {
				return exitError(3, "loading spec: %v", err)
			}

			if errs := spec.Validate(s); len(errs) > 0 {
				var msgs []string
				for _, e := range errs {
					msgs = append(msgs, e.Error())
				}
				return exitError(3, "spec validation failed:\n  %s", strings.Join(msgs, "\n  "))
			}

			complexity := resolveComplexity(complexityFlag, s.EstimatedComplexity, cfg.PhaseMachine.ComplexityDefault)
			if !phase.ValidComplexity(complexity) {
				return exitError(3, "invalid complexity %q, must be TRIVIAL|SMALL|MEDIUM|LARGE", complexity)
			}

			var runtimes []string
			defaultRT, _ := cfg.ResolveRuntime("", "")
			runtimes = append(runtimes, defaultRT)
			for _, rm := range cfg.RoleModels {
				if rm.Runtime != "" {
					runtimes = append(runtimes, rm.Runtime)
				}
			}
			report := gateway.Preflight(gateway.Input{
				Config:     cfg,
				Spec:       s,
				Runtimes:   runtimes,
				Complexity: complexity,
				Mode:       "pipeline",
			})
			if msg := report.FormatStderr(); msg != "" {
				fmt.Fprint(os.Stderr, msg)
			}
			if report.HasErrors() && cfg.Gateway.Strict {
				return exitError(3, "gateway preflight failed")
			}

			store, err := task.NewStore(cfg.TaskDir)
			if err != nil {
				return exitError(1, "creating task store: %v", err)
			}

			emitter, _ := events.NewEmitter(cfg.EventLog)

			r := runner.New(cfg, emitter, store)
			defer r.KillAll()

			specID := strings.TrimSuffix(filepath.Base(specPath), filepath.Ext(specPath))
			sessionPath := session.SessionPath(specID)

			lockPath := filepath.Join(".baton", "sessions", specID+".lock")
			_ = os.MkdirAll(filepath.Dir(lockPath), 0o755)
			lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				return exitError(1, "creating pipeline lock: %v", err)
			}
			defer func() {
				lockFile.Close()
				os.Remove(lockPath)
			}()
			if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
				lockFile.Close()
				return exitError(4, "pipeline already running for spec %s (lock held)", specID)
			}

			allPhases := phase.DefaultPhases()
			active := phase.ActivePhases(allPhases, complexity)
			skipped := phase.SkippedPhaseIDs(allPhases, complexity)

			p := phase.NewPipeline(cfg, r, store, emitter, s, specID, phase.PipelineConfig{
				Complexity: complexity,
			})

			var manifest *session.Manifest
			resuming := false

			if !freshFlag {
				decision, existing := session.CheckResumable(sessionPath, s)
				switch decision.Action {
				case "resume":
					manifest = existing
					manifest.Status = "running"
					manifest.ResumeCount++

					nextPhaseID := findNextPhaseID(active, manifest.LastCompletedPhase())
					if manifest.PhaseResumeAttempts == nil {
						manifest.PhaseResumeAttempts = make(map[int]int)
					}
					manifest.PhaseResumeAttempts[nextPhaseID]++

					maxRetries := cfg.PhaseMachine.MaxL1Retries
					if maxRetries == 0 {
						maxRetries = 2
					}
					maxL2 := cfg.PhaseMachine.MaxL2Cycles
					if maxL2 == 0 {
						maxL2 = 3
					}
					maxL3 := cfg.PhaseMachine.MaxL3Cycles
					if maxL3 == 0 {
						maxL3 = 1
					}

					interruptedName := ""
					interruptReason := "interrupted"
					for _, rec := range manifest.PhaseRecords {
						if rec.Status != "completed" {
							interruptedName = rec.Name
							interruptReason = rec.FailReason
							if interruptReason == "" {
								interruptReason = rec.Status
							}
							break
						}
					}

					briefing := phase.BuildResumeBriefing(
						manifest.PhaseRecords,
						nextPhaseID,
						interruptedName,
						interruptReason,
						manifest.PipelineFiles,
						manifest.RemainingL1Retries(maxRetries+1),
						manifest.RemainingL2Cycles(maxL2),
						manifest.RemainingL3Cycles(maxL3),
					)

					p.SetResume(decision.StartPhase, briefing, manifest.PhaseRecords)
					resuming = true
					fmt.Fprintf(os.Stderr, "Auto-resume: %s\n", decision.Reason)

				case "error":
					return exitError(1, "%s", decision.Reason)

				case "fresh":
					fmt.Fprintf(os.Stderr, "Fresh run: %s\n", decision.Reason)
				}
			}

			if manifest == nil {
				sessionID := fmt.Sprintf("%s-%d", specID, time.Now().Unix())
				manifest = session.New(sessionID, specPath, complexity)
				manifest.SpecCoreHash = session.SpecCoreHash(s)
				if head, err := gitpkg.HeadHash(); err == nil {
					manifest.GitHead = head
				}
			}
			p.SetManifest(manifest, sessionPath)

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
			defer cancel()

			if resuming {
				fmt.Fprintf(os.Stderr, "Pipeline: %s (complexity: %s) [RESUMING from phase %d]\n", specID, complexity, manifest.LastCompletedPhase())
			} else {
				fmt.Fprintf(os.Stderr, "Pipeline: %s (complexity: %s)\n", specID, complexity)
			}
			fmt.Fprintf(os.Stderr, "Phases: %d active, %d skipped\n", len(active), len(skipped))
			fmt.Fprintf(os.Stderr, "Active: %s\n\n", phaseNames(active))

			start := time.Now()
			result, err := p.Run(ctx)
			if err != nil {
				return exitError(1, "pipeline error: %v", err)
			}

			fmt.Fprintf(os.Stderr, "\n--- Pipeline Result ---\n")
			fmt.Fprintf(os.Stderr, "Status:    %s\n", result.Status)
			fmt.Fprintf(os.Stderr, "Duration:  %s\n", result.Duration.Round(time.Second))
			fmt.Fprintf(os.Stderr, "Completed: %v\n", result.PhasesCompleted)
			fmt.Fprintf(os.Stderr, "Skipped:   %v\n", result.PhasesSkipped)
			if result.L2Cycles > 0 {
				fmt.Fprintf(os.Stderr, "L2 Cycles: %d\n", result.L2Cycles)
			}
			if result.FailedPhase != nil {
				fmt.Fprintf(os.Stderr, "Failed at: phase %d\n", *result.FailedPhase)
				fmt.Fprintf(os.Stderr, "Reason:    %s\n", result.FailReason)
			}
			for phaseID, attempts := range result.AttemptsByPhase {
				if attempts > 1 {
					fmt.Fprintf(os.Stderr, "  Phase %d: %d attempts\n", phaseID, attempts)
				}
			}
			if len(result.DirtyFiles) > 0 {
				fmt.Fprintf(os.Stderr, "Dirty files:\n")
				for phaseID, files := range result.DirtyFiles {
					fmt.Fprintf(os.Stderr, "  Phase %d: %v\n", phaseID, files)
				}
			}

			_ = start // used via result.Duration

			switch result.Status {
			case "completed":
				return nil
			case "blocked":
				return exitError(10, "pipeline blocked: %s", result.FailReason)
			case "rate_limited":
				fmt.Fprintf(os.Stderr, "\nSession saved. Re-run to auto-resume: baton pipeline run %s\n", specPath)
				return exitError(11, "pipeline rate limited: %s", result.FailReason)
			default:
				return exitError(1, "pipeline failed: %s", result.FailReason)
			}
		},
	}

	cmd.Flags().StringVar(&complexityFlag, "complexity", "", "Override task complexity (TRIVIAL|SMALL|MEDIUM|LARGE)")
	cmd.Flags().BoolVar(&freshFlag, "fresh", false, "Force a fresh run, ignoring any existing session")

	return cmd
}

func findNextPhaseID(active []phase.Phase, lastCompleted int) int {
	for _, p := range active {
		if p.ID > lastCompleted {
			return p.ID
		}
	}
	return 0
}

func newPipelineStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "status [spec.yaml]",
		Short:         "Show pipeline session status",
		Args:          cobra.MaximumNArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return listSessions()
			}
			specPath := args[0]
			specID := strings.TrimSuffix(filepath.Base(specPath), filepath.Ext(specPath))
			sessionPath := session.SessionPath(specID)

			m, err := session.Load(sessionPath)
			if err != nil {
				fmt.Println("No session found.")
				return nil
			}
			printSessionStatus(m, specPath)
			return nil
		},
	}
}

func listSessions() error {
	entries, err := os.ReadDir(filepath.Join(".baton", "sessions"))
	if err != nil {
		fmt.Println("No pipeline sessions.")
		return nil
	}
	found := false
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(".baton", "sessions", e.Name())
		m, err := session.Load(path)
		if err != nil {
			continue
		}
		found = true
		specID := strings.TrimSuffix(e.Name(), ".yaml")
		fmt.Fprintf(os.Stdout, "%-20s  status=%-12s  phase=%d/%d  resumed=%d\n",
			specID, m.Status, m.Pipeline.CurrentPhase, 16, m.ResumeCount)
	}
	if !found {
		fmt.Println("No pipeline sessions.")
	}
	return nil
}

func printSessionStatus(m *session.Manifest, specPath string) {
	fmt.Fprintf(os.Stdout, "Pipeline Session\n")
	fmt.Fprintf(os.Stdout, "  Spec:     %s\n", m.SpecPath)
	fmt.Fprintf(os.Stdout, "  Status:   %s\n", m.Status)
	fmt.Fprintf(os.Stdout, "  Phase:    %d/16\n", m.Pipeline.CurrentPhase)
	fmt.Fprintf(os.Stdout, "  Started:  %s\n", m.StartedAt.Format(time.RFC3339))
	fmt.Fprintf(os.Stdout, "  Resumes:  %d\n", m.ResumeCount)
	fmt.Fprintln(os.Stdout)

	if len(m.Pipeline.PhasesCompleted) > 0 {
		fmt.Fprintf(os.Stdout, "  Completed: %v\n", m.Pipeline.PhasesCompleted)
	}
	if len(m.Pipeline.PhasesSkipped) > 0 {
		fmt.Fprintf(os.Stdout, "  Skipped:   %v\n", m.Pipeline.PhasesSkipped)
	}
	fmt.Fprintf(os.Stdout, "  Budget:    L1: %d | L2: %d\n",
		m.Budget.L1RetriesTotal, m.Budget.L2CyclesTotal)

	if len(m.PipelineFiles) > 0 {
		fmt.Fprintf(os.Stdout, "  Files:     %s\n", strings.Join(m.PipelineFiles, ", "))
	}

	if m.IsResumable() {
		fmt.Fprintf(os.Stdout, "\n  Resume:   baton pipeline run %s\n", specPath)
	}
}

func resolveComplexity(flag, specValue, configDefault string) string {
	if flag != "" {
		return flag
	}
	if specValue != "" {
		return specValue
	}
	if configDefault != "" {
		return configDefault
	}
	return phase.ComplexityMedium
}

func phaseNames(phases []phase.Phase) string {
	names := make([]string, len(phases))
	for i, p := range phases {
		names[i] = fmt.Sprintf("%d:%s", p.ID, p.Name)
	}
	return strings.Join(names, ", ")
}
