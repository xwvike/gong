package main

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xwvike/gong/internal/config"
	"github.com/xwvike/gong/internal/theme"
)

func TestOverlayArgsCarryAbsoluteTarget(t *testing.T) {
	s := config.Schedule{Label: "mid", At: "00:00:02", Grace: 1200}
	th := theme.Theme{HTML: "/tmp/themes/fake/index.html", Meta: theme.Meta{Lead: 5, Duration: 10}}
	target := time.Date(2026, 7, 27, 0, 0, 2, 0, time.FixedZone("test", 8*60*60))

	args := overlayArgs("/tmp/gong-overlay", s, th, target, "#1")
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

// 壳把认不出来的 flag 当致命错误（退出码 2），所以 Go 侧写错 flag 名不是
// 「少个日志前缀」而是浮层完全不出现。--name 是改名前的旧拼法，防它复活。
func TestOverlayArgsUseTagNotName(t *testing.T) {
	s := config.Schedule{At: "12:00:00", Grace: 1200}
	th := theme.Theme{HTML: "/tmp/themes/fake/index.html"}
	args := overlayArgs("/tmp/gong-overlay", s, th, time.Now(), "#1")

	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--name") {
		t.Errorf("overlay 参数里还有 --name：%s", joined)
	}
	if !strings.Contains(joined, "--tag #1") {
		t.Errorf("overlay 参数里没有 --tag #1：%s", joined)
	}
}

// 没标签时 Ref 会现算出 "#N"，跟前面的序号撞车——这是默认路径，不是边角。
func TestRmPreviewDoesNotRepeatIndex(t *testing.T) {
	s := config.Schedule{At: "12:00:00", Weekdays: []int{1}, Theme: "default"}
	got := rmPreview(2, s)
	if strings.Contains(got, "#2 #2") {
		t.Errorf("没标签时序号打了两遍：%q", got)
	}
	if !strings.HasPrefix(got, "即将删除：#2 12:00:00") {
		t.Errorf("rmPreview = %q", got)
	}

	s.Label = "午间"
	got = rmPreview(2, s)
	if !strings.Contains(got, "标签 午间") {
		t.Errorf("有标签时该带上标签：%q", got)
	}
	if !strings.HasPrefix(got, "即将删除：#2 ") {
		t.Errorf("有标签时序号也得在最前面：%q", got)
	}
}

// 下面三个只测「参数不对」的早退分支——这几支在到达 config.LoadOrDefault()
// 之前就返回了，不会碰真实 HOME 下的配置，更不会走到 install()/launchctl。
// confirmed-删除那条路径特意不在这里自动化：cmdRm 成功时一定会调 install()，
// 而 install() 会真的调 launchctl bootstrap，fake HOME 挡不住它（这个坑已经
// 在 doc.md 和跨会话记忆里写过两遍了）。那条路径靠手动验证，不靠 go test。

func TestCmdRmRejectsMissingArgument(t *testing.T) {
	err := cmdRm(nil)
	if err == nil || !strings.Contains(err.Error(), "要删哪条") {
		t.Fatalf("cmdRm(nil) error = %v，想要「要删哪条」", err)
	}
}

func TestCmdRmRejectsNonNumericArgument(t *testing.T) {
	err := cmdRm([]string{"abc"})
	if err == nil || !strings.Contains(err.Error(), "序号得是个数字") {
		t.Fatalf("cmdRm([abc]) error = %v，想要「序号得是个数字」", err)
	}
}

func TestCmdRmRejectsOutOfRangeIndex(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // 隔离配置文件，但这条分支本来就到不了 install()
	err := cmdRm([]string{"99"})
	if err == nil || !strings.Contains(err.Error(), "没有第 99 条定时") {
		t.Fatalf("cmdRm([99]) error = %v，想要「没有第 99 条定时」", err)
	}
}
