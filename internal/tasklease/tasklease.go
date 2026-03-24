package tasklease

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	SpecVersion      = "v0.1.0"
	DefaultFilesystem = "workspace"
	MaxToolSubset    = 6
)

const (
	StatusOK    ValidationStatus = "OK"
	StatusWarn   ValidationStatus = "WARN"
	StatusError  ValidationStatus = "ERROR"
	CategoryGit  = "Git"
	CategoryTool = "Tools"
	CategoryHand = "Handoff"
)

// Envelope describes a portable task lease for handoff and resume.
type Envelope struct {
	Version     string       `json:"version,omitempty"`
	Name        string       `json:"name,omitempty"`
	Task        string       `json:"task,omitempty"`
	Repo        RepoSpec     `json:"repo,omitempty"`
	ToolSubset  []string     `json:"toolSubset"`
	SecretRefs  []string     `json:"secretRefs"`
	Budget      BudgetSpec   `json:"budget,omitempty"`
	Sandbox     SandboxSpec  `json:"sandbox,omitempty"`
	Artifacts   []string     `json:"artifacts"`
	Resume      ResumeSpec   `json:"resume,omitempty"`
	Git         GitSnapshot  `json:"git,omitempty"`
}

// RepoSpec describes the repository slice covered by the lease.
type RepoSpec struct {
	Revision string `json:"revision,omitempty"`
	Slice    string `json:"slice,omitempty"`
}

// BudgetSpec describes simple execution limits.
type BudgetSpec struct {
	Minutes int `json:"minutes,omitempty"`
	Files   int `json:"files,omitempty"`
}

// SandboxSpec describes the expected execution environment.
type SandboxSpec struct {
	Network    bool   `json:"network,omitempty"`
	Filesystem string `json:"filesystem,omitempty"`
}

// ResumeSpec describes the deterministic resume checkpoint.
type ResumeSpec struct {
	Mode       string `json:"mode,omitempty"`
	Checkpoint  string `json:"checkpoint,omitempty"`
}

// GitSnapshot stores the git facts captured at compile time.
type GitSnapshot struct {
	Head   string `json:"head,omitempty"`
	Dirty  bool   `json:"dirty,omitempty"`
	Prefix string `json:"prefix,omitempty"`
	Branch string `json:"branch,omitempty"`
}

// CompileOptions defines the user input for compiling an envelope.
type CompileOptions struct {
	Name         string
	Task         string
	RepoSlice    string
	Revision     string
	ToolSubset   []string
	SecretRefs   []string
	Artifacts    []string
	BudgetMinutes int
	BudgetFiles   int
	Network      bool
	Filesystem   string
}

// GitState represents live git data collected from the working tree.
type GitState struct {
	Head   string
	Dirty  bool
	Prefix string
	Branch string
}

// ValidationStatus marks the severity of a validation check.
type ValidationStatus string

// ValidationCheck is one category in the validation report.
type ValidationCheck struct {
	Category string           `json:"category"`
	Status   ValidationStatus `json:"status"`
	Issue    string           `json:"issue"`
	Fix      string           `json:"fix,omitempty"`
}

// ValidationReport is the structured output of ValidateEnvelope.
type ValidationReport struct {
	Checks []ValidationCheck `json:"checks"`
}

// FieldChange describes a single field diff between two envelopes.
type FieldChange struct {
	Field string
	Left  string
	Right string
}

// LoadEnvelope reads an envelope from a JSON file.
func LoadEnvelope(path string) (Envelope, error) {
	file, err := os.Open(path)
	if err != nil {
		return Envelope{}, fmt.Errorf("open envelope %q: %w", path, err)
	}
	defer file.Close()

	return DecodeEnvelope(file)
}

// DecodeEnvelope reads an envelope from any JSON stream.
func DecodeEnvelope(r io.Reader) (Envelope, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()

	var env Envelope
	if err := dec.Decode(&env); err != nil {
		return Envelope{}, fmt.Errorf("decode envelope: %w", err)
	}

	return NormalizeEnvelope(env), nil
}

// SaveEnvelope writes a normalized envelope to a JSON file.
func SaveEnvelope(path string, env Envelope) error {
	data, err := MarshalEnvelope(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write envelope %q: %w", path, err)
	}

	return nil
}

// MarshalEnvelope serializes an envelope using stable, readable JSON.
func MarshalEnvelope(env Envelope) ([]byte, error) {
	normalized := NormalizeEnvelope(env)

	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal envelope json: %w", err)
	}

	return append(data, '\n'), nil
}

// NormalizeEnvelope trims strings, removes duplicates, and fills defaults.
func NormalizeEnvelope(env Envelope) Envelope {
	env.Version = strings.TrimSpace(env.Version)
	if env.Version == "" {
		env.Version = SpecVersion
	}

	env.Name = strings.TrimSpace(env.Name)
	env.Task = strings.TrimSpace(env.Task)
	env.Repo.Revision = strings.TrimSpace(env.Repo.Revision)
	env.Repo.Slice = normalizeRepoSlice(env.Repo.Slice)
	env.ToolSubset = normalizeStrings(env.ToolSubset)
	env.SecretRefs = normalizeStrings(env.SecretRefs)
	env.Artifacts = normalizeStrings(env.Artifacts)
	env.Resume.Mode = strings.TrimSpace(env.Resume.Mode)
	env.Resume.Checkpoint = strings.TrimSpace(env.Resume.Checkpoint)
	env.Git.Head = strings.TrimSpace(env.Git.Head)
	env.Git.Prefix = normalizeRepoSlice(env.Git.Prefix)
	env.Git.Branch = strings.TrimSpace(env.Git.Branch)
	env.Sandbox.Filesystem = strings.TrimSpace(env.Sandbox.Filesystem)
	env.Budget = normalizeBudget(env.Budget)
	env.Sandbox = normalizeSandbox(env.Sandbox)

	if env.Name == "" && env.Task != "" {
		env.Name = slugify(env.Task)
	}
	if env.Repo.Slice == "" {
		env.Repo.Slice = "."
	}
	if env.Git.Prefix == "" {
		env.Git.Prefix = "."
	}
	if env.Resume.Mode == "" && env.Resume.Checkpoint != "" {
		env.Resume.Mode = "git"
	}

	return env
}

// CompileEnvelope creates a deterministic envelope from command-line input and git state.
func CompileEnvelope(opts CompileOptions, git GitState) (Envelope, error) {
	task := strings.TrimSpace(opts.Task)
	if task == "" {
		return Envelope{}, fmt.Errorf("task is required")
	}
	if len(opts.ToolSubset) == 0 {
		return Envelope{}, fmt.Errorf("tool subset is required")
	}

	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = slugify(task)
	}

	revision := strings.TrimSpace(opts.Revision)
	if revision == "" {
		revision = strings.TrimSpace(git.Head)
	}
	if revision == "" {
		return Envelope{}, fmt.Errorf("git revision is required")
	}

	repoSlice := normalizeRepoSlice(opts.RepoSlice)
	if repoSlice == "" || repoSlice == "." {
		repoSlice = normalizeRepoSlice(git.Prefix)
	}
	if repoSlice == "" {
		repoSlice = "."
	}

	env := Envelope{
		Version: SpecVersion,
		Name:    name,
		Task:    task,
		Repo: RepoSpec{
			Revision: revision,
			Slice:    repoSlice,
		},
		ToolSubset: normalizeStrings(opts.ToolSubset),
		SecretRefs: normalizeStrings(opts.SecretRefs),
		Artifacts:  normalizeStrings(opts.Artifacts),
		Budget: BudgetSpec{
			Minutes: opts.BudgetMinutes,
			Files:   opts.BudgetFiles,
		},
		Sandbox: SandboxSpec{
			Network:    opts.Network,
			Filesystem: strings.TrimSpace(opts.Filesystem),
		},
		Resume: ResumeSpec{
			Mode:      "git",
			Checkpoint: revision,
		},
		Git: GitSnapshot{
			Head:   strings.TrimSpace(git.Head),
			Dirty:  git.Dirty,
			Prefix: normalizeRepoSlice(git.Prefix),
			Branch: strings.TrimSpace(git.Branch),
		},
	}

	env = NormalizeEnvelope(env)

	if err := validateCompiledEnvelope(env); err != nil {
		return Envelope{}, fmt.Errorf("compile envelope: %w", err)
	}

	return env, nil
}

// LoadGitState reads the current git state from the provided directory.
func LoadGitState(dir string) (GitState, error) {
	root := strings.TrimSpace(dir)
	if root == "" {
		root = "."
	}

	head, err := runGit(root, "rev-parse", "HEAD")
	if err != nil {
		return GitState{}, fmt.Errorf("read git HEAD: %w", err)
	}

	status, err := runGit(root, "status", "--porcelain")
	if err != nil {
		return GitState{}, fmt.Errorf("read git status: %w", err)
	}

	prefix, err := runGit(root, "rev-parse", "--show-prefix")
	if err != nil {
		return GitState{}, fmt.Errorf("read git prefix: %w", err)
	}

	branch, err := runGit(root, "branch", "--show-current")
	if err != nil {
		return GitState{}, fmt.Errorf("read git branch: %w", err)
	}

	return GitState{
		Head:   strings.TrimSpace(head),
		Dirty:  strings.TrimSpace(status) != "",
		Prefix: normalizeRepoSlice(prefix),
		Branch: strings.TrimSpace(branch),
	}, nil
}

// ValidateEnvelope inspects an envelope against a git state and returns a report.
func ValidateEnvelope(env Envelope, git GitState) ValidationReport {
	env = NormalizeEnvelope(env)

	checks := []ValidationCheck{
		validateGitCheck(env, git),
		validateToolCheck(env),
		validateHandoffCheck(env),
	}

	return ValidationReport{Checks: checks}
}

// FormatValidationReport renders the validation report as readable text.
func FormatValidationReport(report ValidationReport) string {
	var b strings.Builder
	for i, check := range report.Checks {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s: %s\n", check.Category, check.Status)
		fmt.Fprintf(&b, "Issue: %s\n", check.Issue)
		if check.Fix != "" {
			fmt.Fprintf(&b, "Fix: %s\n", check.Fix)
		}
	}
	b.WriteString("\n")
	return b.String()
}

// DiffEnvelopes compares two normalized envelopes and returns stable field changes.
func DiffEnvelopes(left, right Envelope) []FieldChange {
	left = NormalizeEnvelope(left)
	right = NormalizeEnvelope(right)

	var changes []FieldChange
	addChange := func(field, leftValue, rightValue string) {
		if leftValue == rightValue {
			return
		}
		changes = append(changes, FieldChange{
			Field: field,
			Left:  leftValue,
			Right: rightValue,
		})
	}

	addChange("version", left.Version, right.Version)
	addChange("name", left.Name, right.Name)
	addChange("task", left.Task, right.Task)
	addChange("repo.revision", left.Repo.Revision, right.Repo.Revision)
	addChange("repo.slice", left.Repo.Slice, right.Repo.Slice)
	addChange("toolSubset", formatList(left.ToolSubset), formatList(right.ToolSubset))
	addChange("secretRefs", formatList(left.SecretRefs), formatList(right.SecretRefs))
	addChange("artifacts", formatList(left.Artifacts), formatList(right.Artifacts))
	addChange("budget.minutes", fmt.Sprintf("%d", left.Budget.Minutes), fmt.Sprintf("%d", right.Budget.Minutes))
	addChange("budget.files", fmt.Sprintf("%d", left.Budget.Files), fmt.Sprintf("%d", right.Budget.Files))
	addChange("sandbox.network", fmt.Sprintf("%t", left.Sandbox.Network), fmt.Sprintf("%t", right.Sandbox.Network))
	addChange("sandbox.filesystem", left.Sandbox.Filesystem, right.Sandbox.Filesystem)
	addChange("resume.mode", left.Resume.Mode, right.Resume.Mode)
	addChange("resume.checkpoint", left.Resume.Checkpoint, right.Resume.Checkpoint)
	addChange("git.head", left.Git.Head, right.Git.Head)
	addChange("git.dirty", fmt.Sprintf("%t", left.Git.Dirty), fmt.Sprintf("%t", right.Git.Dirty))
	addChange("git.prefix", left.Git.Prefix, right.Git.Prefix)
	addChange("git.branch", left.Git.Branch, right.Git.Branch)

	return changes
}

// FormatDiff renders the list of field changes.
func FormatDiff(changes []FieldChange) string {
	if len(changes) == 0 {
		return "No differences detected.\n"
	}

	var b strings.Builder
	for _, change := range changes {
		fmt.Fprintf(&b, "%s\n", change.Field)
		fmt.Fprintf(&b, "Left:  %s\n", change.Left)
		fmt.Fprintf(&b, "Right: %s\n\n", change.Right)
	}

	return b.String()
}

func validateGitCheck(env Envelope, git GitState) ValidationCheck {
	expected := strings.TrimSpace(env.Repo.Revision)
	if expected == "" {
		expected = strings.TrimSpace(env.Resume.Checkpoint)
	}

	if git.Head == "" {
		return ValidationCheck{
			Category: CategoryGit,
			Status:   StatusError,
			Issue:    "git metadata is unavailable.",
			Fix:      "Run tasklease inside a git repository or pass --revision when compiling.",
		}
	}

	if expected == "" {
		return ValidationCheck{
			Category: CategoryGit,
			Status:   StatusError,
			Issue:    "resume checkpoint is missing.",
			Fix:      "Compile the envelope from git so the current HEAD is recorded as the checkpoint.",
		}
	}

	if expected != git.Head {
		return ValidationCheck{
			Category: CategoryGit,
			Status:   StatusError,
			Issue:    fmt.Sprintf("resume checkpoint %q does not match git HEAD %q.", expected, git.Head),
			Fix:      "Recompile the envelope from the current commit before handing it off.",
		}
	}

	if git.Dirty {
		return ValidationCheck{
			Category: CategoryGit,
			Status:   StatusError,
			Issue:    "working tree is dirty.",
			Fix:      "Commit or stash the changes before you hand off the task.",
		}
	}

	return ValidationCheck{
		Category: CategoryGit,
		Status:   StatusOK,
		Issue:    "git HEAD matches the resume checkpoint and the tree is clean.",
	}
}

func validateToolCheck(env Envelope) ValidationCheck {
	if len(env.ToolSubset) == 0 {
		return ValidationCheck{
			Category: CategoryTool,
			Status:   StatusError,
			Issue:    "tool subset is empty.",
			Fix:      "Add at least one supported tool such as git, shell, go, make, fs, or test.",
		}
	}

	if len(env.ToolSubset) > MaxToolSubset {
		return ValidationCheck{
			Category: CategoryTool,
			Status:   StatusError,
			Issue:    fmt.Sprintf("tool subset has %d entries, which exceeds the limit of %d.", len(env.ToolSubset), MaxToolSubset),
			Fix:      "Remove unused tools so the handoff stays small and explicit.",
		}
	}

	var unsupported []string
	for _, tool := range env.ToolSubset {
		if !isSupportedTool(tool) {
			unsupported = append(unsupported, tool)
		}
	}

	if len(unsupported) > 0 {
		return ValidationCheck{
			Category: CategoryTool,
			Status:   StatusError,
			Issue:    fmt.Sprintf("unsupported tools: %s.", strings.Join(unsupported, ", ")),
			Fix:      "Replace them with supported tools: git, shell, go, make, fs, or test.",
		}
	}

	return ValidationCheck{
		Category: CategoryTool,
		Status:   StatusOK,
		Issue:    "tool subset is explicit and supported.",
	}
}

func validateHandoffCheck(env Envelope) ValidationCheck {
	var issues []string
	var fixParts []string

	if env.Resume.Mode != "" && env.Resume.Mode != "git" {
		issues = append(issues, fmt.Sprintf("resume mode %q is not supported", env.Resume.Mode))
		fixParts = append(fixParts, "Use git as the resume mode so checkpoints remain deterministic.")
	}

	if strings.TrimSpace(env.Resume.Checkpoint) == "" {
		issues = append(issues, "resume checkpoint is empty")
		fixParts = append(fixParts, "Compile the envelope from git so the checkpoint is captured automatically.")
	}

	for _, ref := range env.SecretRefs {
		if !isValidSecretRef(ref) {
			issues = append(issues, fmt.Sprintf("secret ref %q is not portable", ref))
			fixParts = append(fixParts, "Use a short environment-like name or a provider URI without whitespace.")
		}
	}

	for _, artifact := range env.Artifacts {
		if !isPortablePath(artifact) {
			issues = append(issues, fmt.Sprintf("artifact path %q is not portable", artifact))
			fixParts = append(fixParts, "Use a repo-relative path without absolute prefixes or parent traversal.")
		}
	}

	if len(env.Artifacts) == 0 {
		return ValidationCheck{
			Category: CategoryHand,
			Status:   StatusWarn,
			Issue:    "no artifact paths are declared.",
			Fix:      "Add at least one output path so the handoff tells the next agent where to write results.",
		}
	}

	if len(issues) > 0 {
		return ValidationCheck{
			Category: CategoryHand,
			Status:   StatusError,
			Issue:    strings.Join(issues, "; ") + ".",
			Fix:      strings.Join(fixParts, " "),
		}
	}

	return ValidationCheck{
		Category: CategoryHand,
		Status:   StatusOK,
		Issue:    "artifact paths and secret refs are portable.",
	}
}

func validateCompiledEnvelope(env Envelope) error {
	if env.Version == "" {
		return fmt.Errorf("version is required")
	}
	if env.Name == "" {
		return fmt.Errorf("name is required")
	}
	if env.Task == "" {
		return fmt.Errorf("task is required")
	}
	if len(env.ToolSubset) == 0 {
		return fmt.Errorf("tool subset is required")
	}
	if strings.TrimSpace(env.Repo.Revision) == "" {
		return fmt.Errorf("repo revision is required")
	}
	if strings.TrimSpace(env.Resume.Checkpoint) == "" {
		return fmt.Errorf("resume checkpoint is required")
	}
	return nil
}

func normalizeStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}

	seen := make(map[string]struct{}, len(values))
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		cleaned = append(cleaned, value)
	}

	sort.Strings(cleaned)
	return cleaned
}

func normalizeBudget(b BudgetSpec) BudgetSpec {
	if b.Minutes < 0 {
		b.Minutes = 0
	}
	if b.Files < 0 {
		b.Files = 0
	}
	return b
}

func normalizeSandbox(s SandboxSpec) SandboxSpec {
	s.Filesystem = strings.TrimSpace(s.Filesystem)
	if s.Filesystem == "" {
		s.Filesystem = DefaultFilesystem
	}
	return s
}

func normalizeRepoSlice(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	cleaned := path.Clean(strings.ReplaceAll(value, "\\", "/"))
	if cleaned == "." {
		return "."
	}
	return cleaned
}

func formatList(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	return "[" + strings.Join(values, ", ") + "]"
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "tasklease"
	}

	var b strings.Builder
	b.Grow(len(value))
	lastHyphen := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastHyphen = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}

	result := strings.Trim(b.String(), "-")
	if result == "" {
		return "tasklease"
	}
	return result
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(out.String()))
	}

	return strings.TrimSpace(out.String()), nil
}

func isSupportedTool(tool string) bool {
	switch tool {
	case "git", "shell", "go", "make", "fs", "test":
		return true
	default:
		return false
	}
}

func isValidSecretRef(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	return !strings.ContainsAny(value, " \t\r\n")
}

func isPortablePath(value string) bool {
	if strings.TrimSpace(value) == "" {
		return false
	}
	if filepath.IsAbs(value) {
		return false
	}

	cleaned := filepath.ToSlash(filepath.Clean(value))
	if cleaned == "." {
		return false
	}
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return false
	}
	return true
}

