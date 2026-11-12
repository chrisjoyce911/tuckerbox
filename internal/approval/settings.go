package approval

// The settings hash (change-settings: Settings that void approval).
//
// An approval also covers the change's own agent and permissions overrides
// in its tuckerbox.toml ([steps.<step>] agent and permissions): a different
// program, or a different permission profile, is not what a person agreed
// to. model, effort and machines are left out, so changing them keeps an
// approval. Only the change's own overrides are hashed, never the effective
// values, so a repository config change voids no approval.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// settingsVersion starts the hashed settings, for the same reason as
// hashVersion.
const settingsVersion = "tuckerbox-settings-v1\n"

// approvedKeys are the step keys an approval covers.
var approvedKeys = []string{"agent", "permissions"}

// SettingsLines returns the agent and permissions overrides in a
// tuckerbox.toml's contents as sorted lines "step.<id>.<key>=<value>". nil
// data (no sidecar) has none.
func SettingsLines(data []byte) ([]string, error) {
	if data == nil {
		return nil, nil
	}
	var doc struct {
		Steps map[string]map[string]any `toml:"steps"`
	}
	if _, err := toml.Decode(string(data), &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", SidecarName, err)
	}
	var lines []string
	for id, t := range doc.Steps {
		for _, k := range approvedKeys {
			v, ok := t[k]
			if !ok {
				continue
			}
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("%s: steps.%s.%s must be a string", SidecarName, id, k)
			}
			lines = append(lines, "step."+id+"."+k+"="+s)
		}
	}
	sort.Strings(lines)
	return lines, nil
}

// SettingsHash returns the settings hash of sorted lines from
// [SettingsLines]: "sha256:" and the hex SHA-256 of each line followed by a
// newline. No lines is the hash a missing settings_hash stands for.
func SettingsHash(lines []string) string {
	h := sha256.New()
	h.Write([]byte(settingsVersion))
	for _, l := range lines {
		h.Write([]byte(l))
		h.Write([]byte{'\n'})
	}
	return Prefix + hex.EncodeToString(h.Sum(nil))
}

// Settings returns the agent and permissions overrides in a change's
// tuckerbox.toml and their hash. A missing sidecar has none.
func Settings(files Files) ([]string, string, error) {
	data, err := files.Read(SidecarName)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		data = nil
	case err != nil:
		return nil, "", err
	}
	lines, err := SettingsLines(data)
	if err != nil {
		return nil, "", err
	}
	return lines, SettingsHash(lines), nil
}

// judgeSettings sets st's state from an approval of the text hash and
// settings hash approved, with the settings lines approvedLines (nil when
// not recorded), against the change's settings now (st.SettingsHash and
// now).
func judgeSettings(st *Status, textHash, settingsHash string, approvedLines []string, now []string) {
	if settingsHash == "" {
		// An approval from before settings were hashed: it covered no
		// overrides.
		settingsHash = SettingsHash(nil)
	}
	known := SettingsHash(approvedLines) == settingsHash
	if !known && settingsHash == SettingsHash(nil) {
		approvedLines, known = nil, true
	}
	st.TextStale = textHash != st.Hash
	if settingsHash != st.SettingsHash {
		st.SettingsChanged = changedKeys(approvedLines, known, now)
	}
	st.State = Approved
	if st.TextStale || len(st.SettingsChanged) > 0 {
		st.State = Stale
	}
}

// changedKeys names the overrides ("step.<id>.<key>") that differ between
// the approved lines and the lines now. When the approved lines are not
// known, every override now is named, or a general word when there is
// none.
func changedKeys(approved []string, known bool, now []string) []string {
	split := func(ls []string) map[string]string {
		m := map[string]string{}
		for _, l := range ls {
			k, v, _ := strings.Cut(l, "=")
			m[k] = v
		}
		return m
	}
	was, is := split(approved), split(now)
	var out []string
	for k, v := range is {
		if !known || was[k] != v {
			out = append(out, k)
		}
	}
	for k := range was {
		if _, ok := is[k]; !ok {
			out = append(out, k)
		}
	}
	if len(out) == 0 {
		out = []string{"agent and permissions overrides"}
	}
	sort.Strings(out)
	return out
}

// StaleReason says why a stale approval no longer holds: the text changed,
// the named overrides changed, or both.
func (s Status) StaleReason() string {
	var why []string
	if s.TextStale {
		why = append(why, "its text changed")
	}
	if len(s.SettingsChanged) > 0 {
		why = append(why, "its "+strings.Join(s.SettingsChanged, ", ")+" changed")
	}
	if len(why) == 0 {
		return "it changed"
	}
	return strings.Join(why, " and ")
}
