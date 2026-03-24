package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ratelworks/tasklease/internal/tasklease"
	"github.com/spf13/cobra"
)

const (
	exitCodeSuccess = 0
	exitCodeUser    = 1
	exitCodeSystem  = 2
)

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	return e.err.Error()
}

func (e *exitError) Unwrap() error {
	return e.err
}

func main() {
	os.Exit(run())
}

func run() int {
	cmd := newRootCommand()
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true

	if err := cmd.Execute(); err != nil {
		var ex *exitError
		if errors.As(err, &ex) {
			fmt.Fprintln(os.Stderr, ex.Error())
			return ex.code
		}

		fmt.Fprintln(os.Stderr, err.Error())
		return exitCodeSystem
	}

	return exitCodeSuccess
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "tasklease",
		Short: "Portable task envelopes for deterministic handoff and resume",
		Long: "tasklease compiles, validates, and diffs small JSON envelopes that capture a git-backed task lease for another agent or developer.",
	}

	root.AddCommand(newCompileCommand())
	root.AddCommand(newValidateCommand())
	root.AddCommand(newDiffCommand())

	return root
}

func newCompileCommand() *cobra.Command {
	var opts tasklease.CompileOptions
	var repoDir string
	var outputPath string

	cmd := &cobra.Command{
		Use:   "compile",
		Short: "Compile a task lease envelope from flags and git state",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(opts.Task) == "" {
				return &exitError{
					code: exitCodeUser,
					err:  fmt.Errorf("compile task lease: task is required"),
				}
			}
			if len(opts.ToolSubset) == 0 {
				return &exitError{
					code: exitCodeUser,
					err:  fmt.Errorf("compile task lease: at least one --tool is required"),
				}
			}

			gitState, err := tasklease.LoadGitState(repoDir)
			if err != nil {
				return &exitError{
					code: exitCodeSystem,
					err:  fmt.Errorf("compile task lease: %w", err),
				}
			}

			env, err := tasklease.CompileEnvelope(opts, gitState)
			if err != nil {
				return &exitError{
					code: exitCodeUser,
					err:  fmt.Errorf("compile task lease: %w", err),
				}
			}

			data, err := tasklease.MarshalEnvelope(env)
			if err != nil {
				return &exitError{
					code: exitCodeSystem,
					err:  fmt.Errorf("compile task lease: %w", err),
				}
			}

			if strings.TrimSpace(outputPath) == "" {
				_, err = cmd.OutOrStdout().Write(data)
				if err != nil {
					return &exitError{
						code: exitCodeSystem,
						err:  fmt.Errorf("compile task lease: write stdout: %w", err),
					}
				}
				return nil
			}

			if err := tasklease.SaveEnvelope(outputPath, env); err != nil {
				return &exitError{
					code: exitCodeSystem,
					err:  fmt.Errorf("compile task lease: %w", err),
				}
			}

			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s\n", outputPath)
			if err != nil {
				return &exitError{
					code: exitCodeSystem,
					err:  fmt.Errorf("compile task lease: write output: %w", err),
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&opts.Name, "name", "", "Human-friendly envelope name")
	cmd.Flags().StringVar(&opts.Task, "task", "", "Task description")
	cmd.Flags().StringVar(&opts.RepoSlice, "slice", "", "Repository slice relative to the git root")
	cmd.Flags().StringVar(&opts.Revision, "revision", "", "Git revision to record")
	cmd.Flags().StringSliceVar(&opts.ToolSubset, "tool", nil, "Allowed tool in the subset")
	cmd.Flags().StringSliceVar(&opts.SecretRefs, "secret", nil, "Secret reference for the handoff")
	cmd.Flags().StringSliceVar(&opts.Artifacts, "artifact", nil, "Artifact path to hand back")
	cmd.Flags().IntVar(&opts.BudgetMinutes, "budget-minutes", 0, "Budget in minutes")
	cmd.Flags().IntVar(&opts.BudgetFiles, "budget-files", 0, "Budget in files")
	cmd.Flags().BoolVar(&opts.Network, "network", false, "Allow network access in the sandbox")
	cmd.Flags().StringVar(&opts.Filesystem, "filesystem", "", "Sandbox filesystem mode")
	cmd.Flags().StringVarP(&repoDir, "repo", "r", ".", "Git repository to inspect")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Write the compiled envelope to a file")

	if err := cmd.MarkFlagRequired("task"); err != nil {
		panic(err)
	}

	return cmd
}

func newValidateCommand() *cobra.Command {
	var repoDir string

	cmd := &cobra.Command{
		Use:   "validate <envelope>",
		Short: "Validate a task lease envelope against the current git state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, err := tasklease.LoadEnvelope(args[0])
			if err != nil {
				return &exitError{
					code: exitCodeSystem,
					err:  fmt.Errorf("validate task lease: %w", err),
				}
			}

			gitState, err := tasklease.LoadGitState(repoDir)
			if err != nil {
				return &exitError{
					code: exitCodeSystem,
					err:  fmt.Errorf("validate task lease: %w", err),
				}
			}

			report := tasklease.ValidateEnvelope(env, gitState)
			_, err = fmt.Fprint(cmd.OutOrStdout(), tasklease.FormatValidationReport(report))
			if err != nil {
				return &exitError{
					code: exitCodeSystem,
					err:  fmt.Errorf("validate task lease: write report: %w", err),
				}
			}

			for _, check := range report.Checks {
				if check.Status == tasklease.StatusError {
					return &exitError{
						code: exitCodeUser,
						err:  fmt.Errorf("validate task lease: envelope has blocking issues"),
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&repoDir, "repo", "r", ".", "Git repository to inspect")
	return cmd
}

func newDiffCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <left> <right>",
		Short: "Diff two task lease envelopes",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			left, err := tasklease.LoadEnvelope(args[0])
			if err != nil {
				return &exitError{
					code: exitCodeSystem,
					err:  fmt.Errorf("diff task lease: load left envelope: %w", err),
				}
			}

			right, err := tasklease.LoadEnvelope(args[1])
			if err != nil {
				return &exitError{
					code: exitCodeSystem,
					err:  fmt.Errorf("diff task lease: load right envelope: %w", err),
				}
			}

			diff := tasklease.DiffEnvelopes(left, right)
			_, err = fmt.Fprint(cmd.OutOrStdout(), tasklease.FormatDiff(diff))
			if err != nil {
				return &exitError{
					code: exitCodeSystem,
					err:  fmt.Errorf("diff task lease: write diff: %w", err),
				}
			}

			return nil
		},
	}

	return cmd
}

