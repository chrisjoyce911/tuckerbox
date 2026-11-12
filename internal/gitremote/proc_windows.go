package gitremote

import (
	"context"
	"os/exec"
	"strconv"
	"time"
)

func configureSSH(c *exec.Cmd) {
	c.Cancel = func() error {
		// Terminate Match-exec descendants too, before killing their SSH parent.
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		kill := exec.CommandContext(ctx, "taskkill", "/T", "/F", "/PID", strconv.Itoa(c.Process.Pid))
		kill.WaitDelay = 500 * time.Millisecond
		if err := kill.Run(); err == nil {
			return nil
		}
		return c.Process.Kill()
	}
}
