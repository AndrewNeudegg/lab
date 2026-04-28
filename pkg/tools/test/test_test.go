package test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBunScriptUsesNixDevShellForFlakeRepoEvenInsideNixShell(t *testing.T) {
	tmp := t.TempDir()
	repoRoot := filepath.Join(tmp, "repo")
	webDir := filepath.Join(repoRoot, "web")
	binDir := filepath.Join(tmp, "bin")
	if err := os.MkdirAll(webDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "flake.nix"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	argsPath := filepath.Join(tmp, "nix.args")
	nixScript := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + argsPath + "\"\nprintf 'nix ok\\n'\n"
	if err := os.WriteFile(filepath.Join(binDir, "nix"), []byte(nixScript), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "bun"), []byte("#!/bin/sh\nexit 42\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", binDir)
	t.Setenv("IN_NIX_SHELL", "impure")

	raw, err := runBunScript(context.Background(), time.Second, repoRoot, webDir, "check")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Command string `json:"command"`
		Output  string `json:"output"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.Command, "nix develop ") {
		t.Fatalf("command = %q, want nix develop", result.Command)
	}
	if strings.TrimSpace(result.Output) != "nix ok" {
		t.Fatalf("output = %q, want fake nix output", result.Output)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	gotArgs := string(args)
	for _, want := range []string{
		"develop\n" + repoRoot + "\n",
		"-c\nbash\n-lc\n",
		"cd \"$1\" && bun install && bun run check\n",
		"bun-tool\n" + webDir + "\n",
	} {
		if !strings.Contains(gotArgs, want) {
			t.Fatalf("nix args = %q, want to contain %q", gotArgs, want)
		}
	}
}

func TestBunScriptTimeoutsHaveReviewSafeMinimums(t *testing.T) {
	if got := atLeastTimeout(time.Second, minBunScriptTimeout); got != 3*time.Minute {
		t.Fatalf("bun script timeout = %s, want 3m", got)
	}
	if got := atLeastTimeout(time.Second, minBunUATTasksTime); got != 5*time.Minute {
		t.Fatalf("task UAT timeout = %s, want 5m", got)
	}
	if got := atLeastTimeout(time.Second, minBunUATSiteTime); got != 10*time.Minute {
		t.Fatalf("site UAT timeout = %s, want 10m", got)
	}
}
