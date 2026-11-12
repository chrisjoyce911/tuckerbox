package agent

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// fakeProbe finds hc when installed, and its server answers when up.
func fakeProbe(installed, up bool) (Probe, *[]string) {
	var calls []string
	return Probe{
		LookPath: func(name string) (string, error) {
			if installed && name == HerdrCore {
				return "/fake/hc", nil
			}
			return "", exec.ErrNotFound
		},
		Output: func(ctx context.Context, path string, args ...string) ([]byte, error) {
			calls = append(calls, path+" "+strings.Join(args, " "))
			if up {
				return []byte(`{"live":0}`), nil
			}
			return []byte("herdr-core server is not running\n"), errors.New("exit status 1")
		},
	}, &calls
}

func TestChooseBackend(t *testing.T) {
	cases := []struct {
		name, setting string
		installed, up bool
		want, reason  string
		err           string
	}{
		{"no herdr-core", "", false, false, "headless", "herdr-core wasn't found (no hc on the PATH)", ""},
		{"auto, server down", "auto", true, false, "headless", "herdr-core isn't running (hc server status: exit status 1: herdr-core server is not running)", ""},
		{"auto, server up", "auto", true, true, "terminal", "herdr-core is running", ""},
		{"headless", "headless", true, true, "headless", `agent_backend is "headless" in machine config`, ""},
		{"terminal", "terminal", true, true, "terminal", `agent_backend is "terminal" in machine config`, ""},
		{"terminal, no hc", "terminal", false, false, "", "", "herdr-core wasn't found"},
		{"terminal, server down", "terminal", true, false, "", "", "herdr-core isn't running"},
		{"unknown", "tmux", true, true, "", "", "unknown agent backend"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, _ := fakeProbe(c.installed, c.up)
			got, err := Choose(c.setting, p)
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("err %v, want %q", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Backend != c.want || got.Reason != c.reason {
				t.Errorf("got %+v, want %s because %q", got, c.want, c.reason)
			}
		})
	}
	// headless never looks for herdr-core.
	p, calls := fakeProbe(true, true)
	if _, err := Choose("headless", p); err != nil || len(*calls) != 0 {
		t.Errorf("headless probed herdr-core: %v %v", *calls, err)
	}
}

func TestHeadlessBackendIsRun(t *testing.T) {
	var b Backend = Headless{}
	if b.Name() != "headless" {
		t.Errorf("name %q", b.Name())
	}
}
