package player

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xwvike/gong/internal/config"
	"github.com/xwvike/gong/internal/theme"
)

func TestScheduledArgs(t *testing.T) {
	s := config.Schedule{At: "00:00:02", Grace: 1200}
	th := theme.Theme{HTML: "/tmp/theme/index.html", Meta: theme.Meta{Lead: 5, Duration: 10}}
	target := time.Date(2026, 7, 27, 0, 0, 2, 0, time.FixedZone("test", 8*60*60))
	args := ScheduledArgs("/tmp/gong-overlay", s, th, target, "#1")
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "--target "+strconv.FormatInt(target.Unix(), 10)) {
		t.Errorf("缺少绝对目标时间：%s", joined)
	}
	if strings.Contains(joined, "--name") || !strings.Contains(joined, "--tag #1") {
		t.Errorf("tag 参数错误：%s", joined)
	}
}
