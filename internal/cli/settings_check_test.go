package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
)

// settingsCheck sets up a repository with the starter config plus repoExtra,
// machine config machine (none if ""), and change add-login with the given
// sidecar, runs tuckerbox check and returns its exit code, stdout and
// stderr.
func settingsCheck(t *testing.T, repoExtra, machine, sidecar string) (int, string, string) {
	t.Helper()
	a, root, out, errOut := configApp(t)
	write := func(path, data string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, config.RepoFile), config.Starter+repoExtra)
	if machine != "" {
		mp, err := config.MachinePath(a.ConfigEnv)
		if err != nil {
			t.Fatal(err)
		}
		write(mp, machine)
	}
	write(filepath.Join(root, "openspec", "changes", "add-login", "tuckerbox.toml"), sidecar)
	code := a.Run([]string{"check"})
	return code, out.String(), errOut.String()
}

const sidecarFile = "openspec/changes/add-login/tuckerbox.toml: change add-login: "

func wantCheckFails(t *testing.T, code int, stderr string, fragments ...string) {
	t.Helper()
	if code == 0 {
		t.Fatalf("check passed; want failure with %q", fragments)
	}
	for _, f := range fragments {
		if !strings.Contains(stderr, f) {
			t.Errorf("stderr lacks %q:\n%s", f, stderr)
		}
	}
}

// repo-config, scenario Machine config sets the allow-list.
func TestCheckMachineConfigSetsAllowList(t *testing.T) {
	code, _, stderr := settingsCheck(t, "", "[overrides]\nagents = [\"codex\"]\n", "")
	wantCheckFails(t, code, stderr, "overrides: [overrides] belongs in repository config")
}

// repo-config, scenario Allowed profile not defined on the machine: the
// list entry alone passes; a change that picks the profile fails, naming
// the missing profile as for a repository step.
func TestCheckAllowedProfileNotOnMachine(t *testing.T) {
	allow := "\n[overrides]\npermissions = [\"edit\"]\n"
	if code, _, stderr := settingsCheck(t, allow, "", "title = \"Login\"\n"); code != 0 {
		t.Fatalf("list entry alone: check exit %d: %s", code, stderr)
	}
	code, _, stderr := settingsCheck(t, allow, "", "[steps.build]\npermissions = \"edit\"\n")
	wantCheckFails(t, code, stderr, sidecarFile+`steps.build.permissions: permission profile "edit" is not defined in machine config`)

	// Defined on the machine for this repository and agent: accepted.
	machine := "[permissions.edit]\nallowed_remotes = [\"*\"]\nargs = { claude = [\"--permission-mode\", \"acceptEdits\"] }\n"
	if code, _, stderr := settingsCheck(t, allow, machine, "[steps.build]\npermissions = \"edit\"\n"); code != 0 {
		t.Errorf("defined profile: check exit %d: %s", code, stderr)
	}
	// Defined, but grants nothing for the step's agent.
	code, _, stderr = settingsCheck(t, allow+"agents = [\"codex\"]\n", machine, "[steps.build]\nagent = \"codex\"\npermissions = \"edit\"\n")
	wantCheckFails(t, code, stderr, `permission profile "edit" grants no flags for agent "codex"`)
}

// change-settings, scenario Override one step: the change's build step is
// accepted with its override; the other steps carry none.
func TestCheckOverrideOneStep(t *testing.T) {
	allow := "\n[overrides]\nagents = [\"claude\", \"codex\"]\n"
	code, stdout, stderr := settingsCheck(t, allow, "", "title = \"Login\"\ndepends_on = []\n\n[steps.build]\nagent = \"codex\"\n\n[approval]\nhash = \"sha256:00\"\n")
	if code != 0 {
		t.Fatalf("check exit %d: %s", code, stderr)
	}
	if !strings.Contains(stdout, "config ok") {
		t.Errorf("stdout: %s", stdout)
	}
}

// change-settings, scenario Unknown step.
func TestCheckUnknownStep(t *testing.T) {
	allow := "\n[overrides]\nagents = [\"claude\", \"codex\"]\n"
	code, _, stderr := settingsCheck(t, allow, "", "[steps.biuld]\nagent = \"codex\"\n")
	wantCheckFails(t, code, stderr, sidecarFile+`steps.biuld: unknown step "biuld"`)

	// A shipping fix step is in [steps], so a change may override it.
	fix := allow + "\n[steps.fix_ci]\nagent = \"claude\"\nrole = \"fixer\"\n\n[shipping]\nfix_ci = \"fix_ci\"\n"
	if code, _, stderr := settingsCheck(t, fix, "", "[steps.fix_ci]\nagent = \"codex\"\n"); code != 0 {
		t.Errorf("fix step: check exit %d: %s", code, stderr)
	}
}

// change-settings, scenario Raw flags in an override: check fails, names
// the key, says flags belong in machine config, and doesn't print the value.
func TestCheckRawFlagsInOverride(t *testing.T) {
	code, _, stderr := settingsCheck(t, "", "", "[steps.build]\nargs = [\"--dangerously-skip-permissions\"]\n")
	wantCheckFails(t, code, stderr, sidecarFile+"steps.build.args: agent arguments and permission flags are not allowed in a change; they belong in a permission profile in machine config")
	if strings.Contains(stderr, "--dangerously-skip-permissions") {
		t.Errorf("value printed:\n%s", stderr)
	}
}

// change-settings, scenario Value not allowed: names change, key, value
// and the allowed values.
func TestCheckValueNotAllowed(t *testing.T) {
	code, _, stderr := settingsCheck(t, "\n[overrides]\nagents = [\"claude\"]\n", "", "[steps.build]\nagent = \"codex\"\n")
	wantCheckFails(t, code, stderr, sidecarFile+`steps.build.agent: agent "codex" is not allowed; allowed agents: claude`)

	// A key the allow-list leaves out may not be overridden.
	code, _, stderr = settingsCheck(t, "\n[overrides]\nagents = [\"claude\"]\n", "", "[steps.build]\neffort = \"high\"\n")
	wantCheckFails(t, code, stderr, sidecarFile+`steps.build.effort: effort "high": the repository allows no change to override effort`)
}

// change-settings, scenario No allow-list.
func TestCheckNoAllowList(t *testing.T) {
	code, _, stderr := settingsCheck(t, "", "", "[steps.review]\neffort = \"low\"\n")
	wantCheckFails(t, code, stderr, sidecarFile+`steps.review.effort: effort "low": the repository allows no change overrides`)
}

// change-settings, scenario Model for another agent: models are allowed
// per agent, judged against the step's effective agent.
func TestCheckModelForAnotherAgent(t *testing.T) {
	allow := "\n[overrides]\nagents = [\"codex\"]\nmodels = { codex = [\"gpt-5.5\"] }\n"
	code, _, stderr := settingsCheck(t, allow, "", "[steps.review]\nmodel = \"gpt-5.5\"\n")
	wantCheckFails(t, code, stderr, sidecarFile+`steps.review.model: model "gpt-5.5" is not an allowed model for "claude"`)

	// With the agent overridden to codex too, the model is allowed.
	if code, _, stderr := settingsCheck(t, allow, "", "[steps.review]\nagent = \"codex\"\nmodel = \"gpt-5.5\"\n"); code != 0 {
		t.Errorf("codex with gpt-5.5: check exit %d: %s", code, stderr)
	}
}

// A sidecar whose settings have the wrong type is reported.
func TestCheckSettingsWrongType(t *testing.T) {
	code, _, stderr := settingsCheck(t, "", "", "machines = \"mini\"\n")
	wantCheckFails(t, code, stderr, "openspec/changes/add-login/tuckerbox.toml: change add-login:")
}
