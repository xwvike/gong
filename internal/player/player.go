package player

import (
	"fmt"
	"os/exec"
	"strconv"
	"time"

	"github.com/xwvike/gong/internal/config"
	"github.com/xwvike/gong/internal/paths"
	"github.com/xwvike/gong/internal/theme"
)

func PreviewCommand(th theme.Theme) (*exec.Cmd, error) {
	overlay := paths.Overlay()
	if overlay == "" {
		return nil, fmt.Errorf("找不到 gong-overlay")
	}
	return exec.Command(overlay,
		"--force",
		"--lead", strconv.Itoa(th.LeadSeconds()),
		"--timeout", strconv.Itoa(th.TimeoutSeconds()),
		"--tag", "vis",
		"--theme", th.HTML,
	), nil
}

func ScheduledArgs(overlay string, s config.Schedule, th theme.Theme, target time.Time, tag string) []string {
	return []string{overlay,
		"--at", config.FormatClock(s.Seconds()),
		"--target", strconv.FormatInt(target.Unix(), 10),
		"--lead", strconv.Itoa(th.LeadSeconds()),
		"--grace", strconv.Itoa(s.Grace),
		"--timeout", strconv.Itoa(th.TimeoutSeconds()),
		"--tag", tag,
		"--theme", th.HTML,
	}
}
