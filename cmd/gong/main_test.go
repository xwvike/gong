package main

import (
	"strings"
	"testing"

	"github.com/xwvike/gong/internal/config"
	"github.com/xwvike/gong/internal/theme"
)

// 没标签时 Ref 会现算出 "#N"，跟前面的序号撞车——这是默认路径，不是边角。
func TestRmPreviewDoesNotRepeatIndex(t *testing.T) {
	s := config.Schedule{At: "12:00:00", Weekdays: []int{1}, Theme: config.DefaultTheme}
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

func TestThemeAttributionUnderlinesOnlySource(t *testing.T) {
	th := theme.Theme{ID: "bloom", Meta: theme.Meta{
		Author: "alice", Source: "https://github.com/alice/original_project",
	}}
	plain := "bloom  @alice  https://github.com/alice/original_project"
	if got := themeAttribution(th, false); got != plain {
		t.Fatalf("非终端输出 = %q", got)
	}
	want := "bloom  @alice  \x1b[4mhttps://github.com/alice/original_project\x1b[24m"
	if got := themeAttribution(th, true); got != want {
		t.Fatalf("终端输出 = %q", got)
	}
}

// 参数错误分支会在读取配置和调用 launchctl 前返回。

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

func TestCmdRmRejectsUnknownFlagAndExtraArguments(t *testing.T) {
	for _, args := range [][]string{{"--wat"}, {"1", "2"}} {
		if err := cmdRm(args); err == nil {
			t.Errorf("cmdRm(%v) should reject invalid arguments", args)
		}
	}
}

func TestCmdVisRejectsExtraArguments(t *testing.T) {
	if err := cmdVis([]string{"led", "extra"}); err == nil || !strings.Contains(err.Error(), "参数太多") {
		t.Fatalf("cmdVis() error = %v, want extra arguments error", err)
	}
}
