// Package paths 集中磁盘位置和运行产物解析。
package paths

import (
	"os"
	"path/filepath"
	"strings"
)

// 由 -ldflags -X 注入
var (
	OverlayPath   string
	BuiltinThemes string
	Version       = "dev"
)

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

func ConfigDir() string  { return filepath.Join(home(), ".config", "gong") }
func ConfigFile() string { return filepath.Join(ConfigDir(), "config.toml") }
func UserThemes() string { return filepath.Join(ConfigDir(), "themes") }

func LaunchAgents() string { return filepath.Join(home(), "Library", "LaunchAgents") }

// Label 是全部定时共用的 launchd job 名称。
const Label = "local.gong"

func PlistFile() string { return filepath.Join(LaunchAgents(), Label+".plist") }

// LegacyLabel 是 0.1.0 用过的一条一个 plist 的命名，升级时要清理掉。
func LegacyLabel(name string) string { return "local.gong." + name }

func LegacyPlistFile(name string) string {
	return filepath.Join(LaunchAgents(), LegacyLabel(name)+".plist")
}

// LogFile 是 launchd 把 stderr 倒进去的地方。主题里的 console.log 和壳的报错都落在这。
func LogFile() string { return filepath.Join(os.TempDir(), "gong.err") }

func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	// Homebrew 的 bin 是软链接，同级资源需要按真实路径查找。
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return filepath.Dir(exe)
}

func firstExisting(candidates ...string) string {
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// Overlay 返回渲染二进制的绝对路径。
func Overlay() string {
	if p := firstExisting(OverlayPath); p != "" {
		return p
	}
	return firstExisting(
		filepath.Join(exeDir(), "gong-overlay"),
		"./gong-overlay",
	)
}

// Builtin 按注入路径、Homebrew 布局和开发布局查找内置主题。
func Builtin() string {
	if p := firstExisting(BuiltinThemes); p != "" {
		return p
	}
	return firstExisting(
		filepath.Join(exeDir(), "..", "share", "gong", "themes"),
		filepath.Join(exeDir(), "themes"),
		"./themes",
	)
}

// StablePath 尽量把 Cellar 路径转换为升级后仍有效的 Homebrew bin 路径。
func StablePath(exe string) string {
	i := strings.Index(exe, "/Cellar/")
	if i < 0 {
		return exe
	}
	prefix := exe[:i] // /opt/homebrew 或 /usr/local
	cand := filepath.Join(prefix, "bin", filepath.Base(exe))
	if _, err := os.Stat(cand); err == nil {
		return cand
	}
	return exe
}
