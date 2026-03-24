package tasklease

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type validationFixture struct {
	Envelope Envelope `json:"envelope"`
	Git      GitState `json:"git"`
}

func TestValidateEnvelopeGolden(t *testing.T) {
	t.Parallel()

	fixtures := []string{
		"case01-clean-envelope",
		"case02-dirty-tree",
		"case03-revision-mismatch",
		"case04-unsupported-tool",
		"case05-too-many-tools",
		"case06-absolute-artifact",
		"case07-bad-secret",
		"case08-no-artifacts",
	}

	for _, fixtureName := range fixtures {
		fixtureName := fixtureName
		t.Run(fixtureName, func(t *testing.T) {
			t.Parallel()

			fixture := loadValidationFixture(t, fixtureName)
			report := ValidateEnvelope(fixture.Envelope, fixture.Git)
			got := strings.TrimSpace(FormatValidationReport(report))

			wantBytes, err := os.ReadFile(filepath.Join("testdata", "validate", fixtureName, "output.txt"))
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}

			want := strings.TrimSpace(string(wantBytes))
			if got != want {
				t.Fatalf("unexpected validation output\nwant:\n%s\ngot:\n%s", want, got)
			}
		})
	}
}

func loadValidationFixture(t *testing.T, name string) validationFixture {
	t.Helper()

	path := filepath.Join("testdata", "validate", name, "input.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}

	var fixture validationFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode fixture %s: %v", path, err)
	}

	return fixture
}

func TestDiffEnvelopes(t *testing.T) {
	t.Parallel()

	left := Envelope{
		Version: SpecVersion,
		Name:    "left",
		Task:    "Compare the left envelope",
		Repo: RepoSpec{
			Revision: "abc123",
			Slice:    ".",
		},
		ToolSubset: []string{"git", "shell"},
		SecretRefs: []string{"TOKEN_A"},
		Artifacts:  []string{"reports/left.md"},
		Resume: ResumeSpec{
			Mode:       "git",
			Checkpoint: "abc123",
		},
	}

	right := Envelope{
		Version: SpecVersion,
		Name:    "right",
		Task:    "Compare the right envelope",
		Repo: RepoSpec{
			Revision: "def456",
			Slice:    "services/api",
		},
		ToolSubset: []string{"git", "shell", "go"},
		SecretRefs: []string{"TOKEN_A", "TOKEN_B"},
		Artifacts:  []string{"reports/right.md"},
		Resume: ResumeSpec{
			Mode:       "git",
			Checkpoint: "def456",
		},
	}

	changes := DiffEnvelopes(left, right)
	if len(changes) < 5 {
		t.Fatalf("expected several changes, got %d", len(changes))
	}

	diff := FormatDiff(changes)
	if !strings.Contains(diff, "toolSubset") {
		t.Fatalf("expected diff output to mention toolSubset, got:\n%s", diff)
	}
}

func TestLoadGitState(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Tasklease Test",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Tasklease Test",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
		}
		return string(out)
	}

	run("init")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("tasklease"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	run("add", "README.md")
	run("commit", "-m", "initial")

	state, err := LoadGitState(dir)
	if err != nil {
		t.Fatalf("LoadGitState failed: %v", err)
	}
	if state.Head == "" {
		t.Fatal("expected git head to be populated")
	}
	if state.Dirty {
		t.Fatal("expected clean git state")
	}
}
