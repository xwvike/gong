package main

import (
	"strconv"
	"testing"
	"time"

	"github.com/xwvike/gong/internal/config"
	"github.com/xwvike/gong/internal/theme"
)

func TestOverlayArgsCarryAbsoluteTarget(t *testing.T) {
	s := config.Schedule{Name: "mid", At: "00:00:02", Grace: 1200}
	th := theme.Theme{HTML: "/tmp/themes/nixie/index.html", Meta: theme.Meta{Lead: 5, Duration: 10}}
	target := time.Date(2026, 7, 27, 0, 0, 2, 0, time.FixedZone("test", 8*60*60))

	args := overlayArgs("/tmp/gong-overlay", s, th, target)
	for i, arg := range args {
		if arg != "--target" {
			continue
		}
		if i+1 >= len(args) {
			t.Fatal("--target 缺少参数")
		}
		if got, want := args[i+1], strconv.FormatInt(target.Unix(), 10); got != want {
			t.Errorf("--target = %q，想要绝对 Unix 时间 %q", got, want)
		}
		return
	}
	t.Fatal("overlay 参数里没有 --target")
}
