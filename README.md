# gong（锣）

到点在所有屏幕最顶层播一段 HTML 动画，宣告某个时刻到了，播完自动退出。

不抢焦点（你正在打字，光标纹丝不动）、不吃点击（鼠标当它不存在）、背景真透明（盖在桌面上而不是盖住桌面）、**常驻进程数为 0**。

## 装

```bash
brew install xwvike/tap/gong
gong on
```

`gong on` 之后立刻可用，不用重启也不用登出。默认两条定时：中午 12:00、傍晚 18:00，周一到周五。

## 卸

```bash
gong uninstall
```

清 plist、从 launchd 撤出，最后自动帮你跑 `brew uninstall`。加 `--purge` 连
`~/.config/gong`（含你自己写的主题）一起删，加 `-y` 跳过确认。

**别直接 `brew uninstall gong`** —— formula 没有 uninstall hook，
`~/Library/LaunchAgents` 里的 plist 会留下来，每天到点去拉一个不存在的二进制，
而且是静默失败。

只想暂停一阵子、程序留着：`gong off`。

装完你会在「系统设置 → 通用 → 登录项与扩展 → 允许在后台」里看到**一个**叫 gong 的条目。
不管配了多少条定时都只有一个，而且它**不会在登录时运行任何东西**（`RunAtLoad` 是 `false`），
只是告诉 launchd 到点叫醒一次。

## 用

```bash
gong set            # TUI：增删改查定时、换主题、预览
gong ls             # 看看现在有哪些定时，以及是不是真的接管了
gong vis default    # 预览一个主题
gong stop           # 掐掉正在播的浮层
gong themes         # 列出可用主题
```

## 主题

标准主题就是一个目录：`index.html` 负责表现，`theme.toml` 负责给 Go 提供元数据。放进 `~/.config/gong/themes/<name>/` 就能用，同名会盖住内置主题；旧主题可以省略 `theme.toml` 并使用默认元数据。

```toml
name      = "白炽灯丝"
desc      = "六位钨丝时钟由暗红升至暖白，到点后留下冷却余辉。"
lead      = 5        # 提前多少秒亮相（launchd 只有分钟精度，剩下的由壳自己等）
duration  = 10       # 从实际亮相到视觉预计结束的总秒数
placement = "center" # center / edge / corner，目前只用于遮挡范围提示
webgl     = true     # 仅声明主题会自行尝试 WebGL；上下文及降级由主题自己负责
```

外壳会在 `.atDocumentStart` 注入固定的 **Theme API v1**。下面这些字段和回调槽始终存在；完整类型、单位、不变量、生命周期和兼容规则见 [Theme API v1](doc.md#theme-api-v1)。

```js
// 宿主注入的固定形状（示意）；主题不要执行这段赋值或替换 window.gong。
window.gong = {
  apiVersion: 1,
  target: 1753588800000, now: 1753588795012, lead: 5, force: false,
  revealed: false, fired: false, screens: 1,
  screen: {index: 0, isMain: true, primary: true, w: 1512, h: 982, scale: 2},
  onReveal: null, onTick: null, onFire: null,
  done() {}
};

const g = window.gong;
g.onReveal = () => { /* 壳亮相后开始表现 */ };
g.onTick = now => { /* 用绝对时间差推进，不要数心跳 */ };
g.onFire = () => { /* 到达目标时刻，或迟到后立即补发 */ };
// 退场动画完成后再调用；定义函数不会让主题加载时立即退出：
const finish = () => g.done();
```

多屏主题可用 `window.gong.screen.primary` 判断当前实例是不是主屏；不要假设 `screen.index === 0` 一定是主屏。

三条要紧的：

1. **动画挂在 `html.gong-live` 下面**。页面是被提前加载好的，按 `load` 起算会跑偏。到点后还会多一个 `html.gong-fired`，纯 CSS 主题可以一行 JS 都不写。
2. **别用 `setTimeout` / `setInterval` / `requestAnimationFrame` 当时钟或退出闸门**，用 `onTick(now)` 的绝对时间和 `onFire`。`requestAnimationFrame` 可以画帧，但不能决定是否到点。
3. **常驻 CSS 动画别挂在带 `filter` 的元素上**，挂容器上。前者会把主线程压死。

Go 只读取 `theme.toml`、计算调度参数并启动 Swift 外壳；Swift 再把时间和屏幕快照组装成上面的固定对象。HTML 不会直接收到 TOML、定时名称或提醒文案，`webgl = true` 也不会替页面创建 WebGL 上下文。

调试直接 `gong vis <theme>`——预览和真实触发走完全同一条渲染路径。主题的 console、同步回调异常和未处理的 Promise rejection 都会打到 stderr。纯 CSS 主题可以不注册回调，但只能等外壳 timeout 退出；需要精确收尾就由主屏调用 `done()`。

## 为什么不是别的

`mpv --fullscreen --ontop` 和 Chrome `--app` 都会激活自己的 App、抢焦点；Tauri 的 `alwaysOnTop` 只到 floating 层，盖不住别的 App 的全屏窗口；Electron 方案常驻 150–250MB。

最后是一个 124K 的 Swift 壳（`NSPanel` + `.nonactivatingPanel` + screenSaver 层）加 WKWebView：窗口语义用原生，动画用 HTML/CSS 写，改效果不用重新编译。

设计取舍和踩过的坑都在 [doc.md](doc.md)。

## 自己编

```bash
swiftc -O overlay.swift -o gong-overlay
go build -o gong ./cmd/gong
./gong vis default
```

需要 Xcode Command Line Tools（`swiftc` 在里面，不需要完整 Xcode）和 Go。走 brew 装的话这两个都不用。
