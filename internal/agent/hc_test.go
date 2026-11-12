package agent

import (
	"testing"
	"time"
)

// agent-activity: Times and last activity. A terminal's last activity is
// its last output; a later status change (the agent going quiet, or
// waiting) doesn't replace it. Status and start are only fallbacks.
func TestLastActivityPrefersOutput(t *testing.T) {
	ms := func(v int64) *int64 { return &v }
	const created, output, status = 1_700_000_000_000, 1_700_000_600_000, 1_700_000_900_000
	cases := []struct {
		name string
		t    *HCTerminal
		want int64
	}{
		{"status after output", &HCTerminal{CreatedMs: ms(created), OutputMs: ms(output), StatusMs: ms(status)}, output},
		{"no output", &HCTerminal{CreatedMs: ms(created), StatusMs: ms(status)}, status},
		{"only start", &HCTerminal{CreatedMs: ms(created)}, created},
		{"zero output", &HCTerminal{CreatedMs: ms(created), OutputMs: ms(0)}, created},
	}
	for _, c := range cases {
		if got := c.t.LastActivity(); !got.Equal(time.UnixMilli(c.want)) {
			t.Errorf("%s: %v, want %v", c.name, got, time.UnixMilli(c.want))
		}
	}
	if got := (&HCTerminal{}).LastActivity(); !got.IsZero() {
		t.Errorf("nothing reported: %v", got)
	}
	var none *HCTerminal
	if got := none.LastActivity(); !got.IsZero() {
		t.Errorf("nil: %v", got)
	}
}
