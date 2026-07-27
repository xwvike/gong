# gong（锣）

到点在所有屏幕最顶层播一段 HTML 动画，宣告某个时刻到了，播完自动退出。

不抢焦点（你正在打字，光标纹丝不动）、不吃点击（鼠标当它不存在）、背景真透明（盖在桌面上而不是盖住桌面）、**常驻进程数为 0**。

## 装

```bash
brew install xwvike/tap/gong
gong on
```

`gong on` 之后立刻可用，不用重启也不用登出。默认两条定时：中午 12:00、傍晚 18:00，周一到周五。

**卸载前请先 `gong off`** —— `brew uninstall` 不会清 `~/Library/LaunchAgents` 里的 plist。

装完你会在「系统设置 → 通用 → 登录项与扩展 → 允许在后台」里看到**一个**叫 gong 的条目。
不管配了多少条定时都只有一个，而且它**不会在登录时运行任何东西**（`RunAtLoad` 是 `false`），
只是告诉 launchd 到点叫醒一次。

## 用

```bash
gong set            # TUI：增删改查定时、换主题、预览
gong ls             # 看看现在有哪些定时，以及是不是真的接管了
gong vis nixie      # 预览一个主题
gong stop           # 掐掉正在播的浮层
gong themes         # 列出可用主题
```

## 主题

主题就是一个目录，`index.html` + `theme.toml`。放进 `~/.config/gong/themes/<name>/` 就能用，同名会盖住内置的。

```toml
lead      = 5        # 提前多少秒亮相（launchd 只有分钟精度，剩下的由壳自己等）
duration  = 10       # 预期可见时长
placement = "center" # center / edge / corner
```

页面里能拿到 `window.gong`：

```js
window.gong.onReveal = () => { /* 壳亮相了，动画从这里起跑 */ };
window.gong.onFire   = () => { /* 到点了 */ };
window.gong.onTick   = (now) => { /* 壳每 100ms 喂一次时间 */ };
window.gong.done();                     // 放完了，通知壳退出
```

三条要紧的：

1. **动画挂在 `html.gong-live` 下面**。页面是被提前加载好的，按 `load` 起算会跑偏。到点后还会多一个 `html.gong-fired`，纯 CSS 主题可以一行 JS 都不写。
2. **别用 `setTimeout` / `setInterval` / `requestAnimationFrame` 卡点**，用 `onTick` / `onFire`。页面里的定时器在重绘压力下会被拖到几秒甚至不来，壳这边误差只有几毫秒。
3. **常驻 CSS 动画别挂在带 `filter` 的元素上**，挂容器上。前者会把主线程压死。

调试直接 `gong vis <theme>`——预览和真实触发走完全同一条渲染路径。主题里的 `console.log` 和未捕获异常会打到 stderr。

## 为什么不是别的

`mpv --fullscreen --ontop` 和 Chrome `--app` 都会激活自己的 App、抢焦点；Tauri 的 `alwaysOnTop` 只到 floating 层，盖不住别的 App 的全屏窗口；Electron 方案常驻 150–250MB。

最后是一个 124K 的 Swift 壳（`NSPanel` + `.nonactivatingPanel` + screenSaver 层）加 WKWebView：窗口语义用原生，动画用 HTML/CSS 写，改效果不用重新编译。

设计取舍和踩过的坑都在 [doc.md](doc.md)。

## 自己编

```bash
swiftc -O overlay.swift -o gong-overlay
go build -o gong ./cmd/gong
./gong vis nixie
```

需要 Xcode Command Line Tools（`swiftc` 在里面，不需要完整 Xcode）和 Go。走 brew 装的话这两个都不用。
