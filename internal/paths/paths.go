// Package paths 集中所有磁盘位置和「产物在哪」的解析。
//
// overlayPath / builtinThemes 由 formula 在编译期用 ldflags 注入，指向 opt 前缀
// （/opt/homebrew/bin/... 而不是 Cellar 里带版本号的路径，后者 brew upgrade 后就死了）。
// 开发时这两个是空的，回落到「跟可执行文件同目录」再回落到当前目录。
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

// Label 是唯一那个 launchd job 的名字。
//
// 所有定时共用一个 plist，不是一条一个。原因是 macOS 13 之后会把每个第三方
// LaunchAgent 单独列进「登录项与扩展 → 允许在后台」，一条定时一个 plist 就意味着
// N 条定时在系统设置里排出 N 个一模一样的「gong」，用户既分不清也可能顺手关掉一个
// ——关掉之后我们完全无从知晓，那条提醒就静悄悄地不响了。
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
	// 走一次 EvalSymlinks：brew 的 bin 是软链，但我们要的是真实同级目录
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

// Builtin 返回内置主题目录。内置主题留在 brew 前缀里不 copy 到 home，
// 这样 brew upgrade 能更新它们。
//
// 三个候选按顺序试：ldflags 注入的、brew 的标准布局（bin/../share/gong/themes）、
// 开发时的同级 themes。有了第二条，formula 就不需要 wrapper 脚本也不需要注入。
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

// StablePath 把「当前可执行文件」换算成一个写进 plist 也不会烂的路径。
//
// os.Executable() 在从 PATH 调用时给的是 /opt/homebrew/bin/gong，那个是稳的；
// 但要是拿到了 Cellar 里带版本号的路径，brew upgrade 之后 launchd 就会指向
// 一个不存在的文件，而且是静默失效——每天到点什么都不发生。
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
