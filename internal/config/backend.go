package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/BurntSushi/toml"
)

// Agent backends, the values of machine config's agent_backend.
//
//   - BackendAuto (the default): the terminal backend when herdr-core's hc
//     is on the PATH and its server answers, else headless.
//   - BackendTerminal: agents run in herdr-core terminals; a run refuses
//     to start when herdr-core is not running.
//   - BackendHeadless: agents run as child processes of Tuckerbox.
const (
	BackendAuto     = "auto"
	BackendTerminal = "terminal"
	BackendHeadless = "headless"
)

// CheckBackend reports whether v is a valid agent_backend value.
func CheckBackend(v string) error {
	switch v {
	case BackendAuto, BackendTerminal, BackendHeadless:
		return nil
	}
	return fmt.Errorf("unknown agent backend %q; use \"auto\", \"terminal\" or \"headless\"", v)
}

// MachineBackend reads agent_backend from the machine config file at path,
// without needing a repository (for doctor). A missing file, or a file
// that does not set it, gives [BackendAuto]. Other keys are not checked;
// `tuckerbox check` does that.
func MachineBackend(path string) (string, error) {
	if path == "" {
		return BackendAuto, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return BackendAuto, nil
	}
	if err != nil {
		return "", err
	}
	var raw struct {
		AgentBackend *string `toml:"agent_backend"`
	}
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	if raw.AgentBackend == nil {
		return BackendAuto, nil
	}
	if err := CheckBackend(*raw.AgentBackend); err != nil {
		return "", fmt.Errorf("%s: agent_backend: %w", path, err)
	}
	return *raw.AgentBackend, nil
}
