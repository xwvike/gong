# gong 工程说明

面向改这个仓库的人。README 讲怎么用，这份讲**为什么是现在这个样子**，以及改动时哪些边界不能碰。

内容以当前代码为准。写进来的每个常量、路径、顺序都能在源码里找到对应位置；**发现对不上，以代码为准并顺手改这份文档**，不要在这里堆历史。

---

## 1. 三层结构

```
launchd ──> gong fire ──exec──> gong-overlay ──> WKWebView ──> themes/<id>/index.html
            (Go CLI)            (Swift 壳)                      (主题)
```

| 层 | 文件 | 职责 | 明确不做的事 |
|---|---|---|---|
| Go CLI | `cmd/gong`, `internal/*` | 配置、调度、主题解析与选择、launchd 生命周期 | 不画任何像素 |
| Swift 壳 | `overlay.swift` | 开窗、时间闸门、抑制判断、给页面喂时间 | **不读配置文件，不猜安装目录**，只认命令行 flag |
| 主题 | `themes/<id>/` | 全部视觉表现 | 不读配置，不碰文件系统，不与其它主题共享代码 |

三条边界的用处：

- **配置只有一个真相（Go）**。壳不读 `config.toml`，所以调度语义的改动永远只落在 Go 侧，不会出现两边解析不一致。
- **主题之间零耦合**。每个主题是一个自包含的 `index.html`，没有共享的 js/css。复制一个主题去改，不会影响别的主题；删掉一个主题，不会有任何东西失效。
- **预览和真实触发是同一条路径**。`gong vis` 只是多加了 `--force`（见 `cmdVis`），没有第二套渲染逻辑。预览里对的，真触发时也对。

---

## 2. 一次触发的完整路径

1. launchd 在 `StartCalendarInterval` 的某个分钟槽拉起 `gong fire`。
2. `cmdFire` 读配置 → `agent.MatchTarget(c, time.Now())` **反查这一下是哪条定时**。
   launchd 不会告诉我们是谁触发的（所有定时共用一个 job），所以按时间往回找最近的触发点。
   猜错也不至于出事——壳自己的时间窗判断会把不该放的挡掉。
3. `theme.Select(...)` 按定时的主题字段选出本次实际主题（固定名 / `@random` / `@sequence`）。
4. `player.ScheduledArgs` 拼出 flag，`syscall.Exec` **换掉自己的进程映像**跑 `gong-overlay`。
   用 exec 而不是 fork：launchd 里只留一个进程，`gong stop` 一个 `pkill -x gong-overlay` 就能全掐掉。
5. 壳解析 flag → 时间窗与全屏判断 → 每块屏建一个 `OverlayPanel` + `WKWebView`，**先加载不展示**。
6. 到 `target - lead` 调 `reveal()`：`orderFrontRegardless()` 展示所有面板，派发 `onReveal`，并启动 100ms 心跳。
7. 到 `target` 调 `fire()`：派发 `onFire`。
8. 主题自己决定何时演完，调 `gong.done()` → 壳 `NSApp.terminate`。

传给壳的 flag 由 `player.ScheduledArgs` 生成：`--at --target --lead --grace --timeout --tag --theme`。
其中 `--target` 是**绝对 Unix 秒**，`--at` 只为兼容和手工调试保留——跨午夜的定时按「今天」解析 `--at` 会算错，所以绝对时刻由 Go 侧算好传进来。

---

## 3. 调度

### 所有定时共用一个 plist

`~/Library/LaunchAgents/local.gong.plist`，label `local.gong`（`paths.Label`）。
配几条定时，系统设置的「登录项与扩展」里都只有一个 gong。

代价是 launchd 不告诉我们是哪个槽触发的，所以有了第 2 节的反查。这个交换是划算的：用户看到一堆重复的后台项会怀疑软件有问题。

`0.1.0` 曾经是一条定时一个 plist，`agent.legacyInstalled()` 至今仍在清理那批 `local.gong.<name>.plist`。注意它必须排掉 `local.gong.plist` 自己——这个文件名同样符合 `local.gong.*.plist` 的形状，不排掉会把刚写好的那份删掉。

### 分钟精度与提前唤醒

launchd 只有分钟精度，而定时是秒级的（`at = "18:00:00"`），主题还要提前 `lead` 秒亮相。
`agent.ComputeTrigger` 把「几点几分几秒 − lead 秒」**向下取整到分钟**，宁可早也不能晚：

```
at=18:00:00, lead=4  →  launchd 在 17:59 叫醒，壳自己等到 17:59:56 才 reveal
at=00:00:02, lead=5  →  跨回前一天 23:59（dayShift=-1，星期几也跟着挪）
```

`targetForTrigger` 用**同一套跨日偏移规则**把绝对目标时刻还原回来，两边必须对称，否则跨午夜的定时会错一天。

### 分钟槽冲突

两条定时算出同一个 `(weekday, hour, minute)` 时，launchd 只会叫醒一次，反查也只能命中一条 —— 另一条会静默失效。所以 `agent.ValidateTriggerSlots` **在保存前就拒绝**这种配置，而不是让它悄悄坏掉。

`agent.Plist` 里另有一层去重：即便校验漏过，写进 plist 的触发点也不会重复，避免 launchd 叫醒两次、屏幕上叠两个浮层。

### 写盘是原子的

`config.Save` 和 `agent.writePlist` 都走「同目录临时文件 → fsync → rename」。
plist 尤其关键：launchd 可能在写入中途读到半份 XML。

### Sync 是幂等的

`agent.Sync` 可以随便重复跑。一条启用的定时都没有时，它会把 plist 整个撤掉，而不是在系统里留一个空 job。
`Bootstrap` 先 `bootout` 再 `bootstrap`——对已加载的 label 直接 bootstrap 会报 `Bootstrap failed: 5: Input/output error`。

---

## 4. 主题选择策略

定时的 `theme` 字段有三种取值（`internal/theme/selection.go`）：

| 值 | 行为 |
|---|---|
| `<id>` | 固定主题 |
| `@random` | 按 `(定时序号, target 的 Unix 秒, 全部候选 ID)` 做 sha256，取模选一个 |
| `@sequence` | 按「从固定纪元到 target 之间实际触发过多少次」取模，逐次轮换 |

两个策略都是**确定性**的：同一个 target 反复计算得到同一个主题。`gong ls` 的预览、TUI 里的预览和真正触发因此永远一致。

`@random` 不用 `math/rand`：进程每次触发都是新起的，用随机数源就没法复现，排查「昨天下班那个是哪个主题」会变成不可能。

`@sequence` 的 `occurrenceOrdinal` 只统计**被选中的星期**，所以周五之后的下一个周一恰好递增一次，而不是跳过周末的两次。

### 唤醒提前量要按最慢的算

`theme.WakeLead` 对动态策略返回**所有候选主题里最大的 lead**。
launchd 只能按一个固定分钟叫醒，而这一刻还不知道会抽到哪个主题。按最慢的唤醒，真正播放时由选中的主题自己等到它的亮相时刻——反正壳的 `reveal` 定时器是按 `target - lead` 算的。

---

## 5. Theme API v1

壳在 `.atDocumentStart` 注入 `window.gong`（`overlay.swift: bootstrapJS`）。`apiVersion` 只表示这份契约的主版本，**不跟 gong 的发布版本走**。

### 只读字段

| 字段 | 类型 | 含义 |
|---|---|---|
| `apiVersion` | `1` | 契约主版本，`Object.defineProperty` 锁死 |
| `target` | epoch ms | 约定的那一刻 |
| `now` | epoch ms | 壳最近一次喂进来的时间，每次回调前刷新 |
| `lead` | 秒 | 提前亮相量，主题用它算入场曲线的时长 |
| `force` | bool | 预览模式（`gong vis`） |
| `revealed` / `fired` | bool | 状态镜像 |
| `screens` | int | 屏幕总数 |
| `screen` | `{index, isMain, primary, w, h, scale}` | 本页所在屏 |

时间一律是 **epoch 毫秒的 JS number**。`epochMilliseconds` 和 JS 侧的 `normalizeMillis` 都会校验安全整数范围，越界直接退出而不是悄悄传个坏值。

### 回调槽

`onReveal` / `onTick(now)` / `onFire`，初始都是 `null`——`null` 表示主题不关心这个事件。
赋成非函数的值会被 `invoke` 报到 stderr（1 秒去重），不会静默失效。回调抛异常或返回 rejected promise 同样会被捕获上报。

### 生命周期顺序（有测试守着）

```
reveal ──> [tick, tick, ...] ──> fire ──> [tick, ...] ──> done()
```

三条硬保证，改壳时不能破：

1. **`fire` 永远在 `reveal` 之后。** `fire()` 里 `if !revealed { reveal() }`；JS 侧 `__gongFire` 里也有 `if (!didReveal) window.__gongReveal(ms)`。
2. **`target` 之后的第一个 tick 之前，`fire` 已经派发过。** 心跳回调开头就有 `if !self.fired, Date() >= self.target { self.fire() }`。主题因此可以放心地在 `onTick` 里判断 `now >= target`，不用担心 `onFire` 还没来。
3. **页面加载完成与 reveal 谁先谁后是不定的。** `lead = 0` 时 reveal 几乎立刻发生，那会儿 `__gongReveal` 还不存在。所以 `webView(_:didFinish:)` 会按 `reveal → fire` 的顺序为迟到的那一屏补发。

`internal/theme/api_contract_test.go` 直接读 `overlay.swift` 的源码断言这些顺序。它不编译 Swift，只做文本顺序检查——**改这几段时如果测试挂了，先想清楚顺序是不是真的还成立，再去改测试里的 token**。

### `done()` 的语义

```js
gong.done = function () {
  if (doneSent || !gong.screen.primary) return;
  ...
};
```

多屏时每块屏都在独立跑同一份主题。**只有主屏的 `done()` 算数**，非主屏调用会被安静吃掉。
所以主题里可以无脑写 `g.done()`，不需要自己判断 `screen.primary`——现有五个主题都是这么写的。

壳侧还有第二道校验：消息必须来自主 frame 且来自 `primaryWeb`。

### console 桥

`console.log/warn/error`、`window.onerror`、`unhandledrejection` 全部转发到壳的 stderr，格式：

```
gong-overlay[#2][js] log 这是主题打的日志
```

`[#2]` 是 `--tag`（定时序号，或预览时的 `vis`），只用于打标，**不是身份**。

launchd 跑的时候 stderr 落在 `paths.LogFile()`，也就是 `$TMPDIR/gong.err`。这是调主题最有用的一条通道，尤其是截图/录屏受权限限制的时候。

---

## 6. 主题里的时钟

### 权威时间归壳，帧间插值归主题

心跳约每 100ms 一次，允许跳帧。**主题必须用 `now` 的绝对差值计时，不能数心跳次数。**

100ms 是「时刻」的粒度，不是「画面」的粒度。要满帧动画就自己开 `requestAnimationFrame`。

### 锚点时钟

需要满帧动画时，用心跳校准绝对时间、`performance.now()` 补帧：

```js
let anchorGong = 0, anchorPerf = 0, lastClock = 0;
function clockNow() {
  const t = anchorGong + (performance.now() - anchorPerf);
  if (t > lastClock) lastClock = t;   // 单调不回退：系统校时可能让 epoch 暂时倒退
  return lastClock;
}

// onReveal： anchorGong = g.now; anchorPerf = performance.now(); lastClock = g.now;
// onTick：   if (now > anchorGong) { anchorGong = now; anchorPerf = performance.now(); }
```

配套的两条纪律：

- **rAF 和 onTick 调用同一个 `update(now)`**，它只依赖传进来的绝对时间，谁先到结果都一致。
- **`done()` 只从 `onTick` 发。** rAF 万一被系统节流或停掉，心跳仍能推进退出，主题不会挂到壳的兜底超时。

### 现有主题的做法

| 主题 | 画面驱动 | 生命周期驱动 |
|---|---|---|
| `led` | rAF（滚动插值，自己算 dt 并 clamp 到 50ms） | `onTick` |
| `tunnel` | CSS transition，无 rAF | `onTick` + `onFire` |
| `noise` | rAF + 锚点时钟 | `onTick` |
| `bloom` | rAF + 锚点时钟（WebGL） | `onTick` |
| `chroma` | rAF + 锚点时钟（WebGL） | `onTick` |

`led` 显示的是**滚动到那一帧的墙上时间**（`onTick` 里按分钟重建列），`noise` 显示的是 `g.target`——约定的那个点，整场不变，所以离屏画布只画一次。两种都合理，看主题想表达什么。

### 降级路径

每个主题都得能在两种情况下正常退场：

- **`prefers-reduced-motion: reduce`**：画一帧静态的就够，但**退出判断不能一起被跳过**。`bloom` 早期版本把退出检查写在 reduced 的 early return 之后，结果一直挂到壳的可见兜底超时才被收掉。
- **拿不到 WebGL**（`bloom` / `chroma`）：警告一声，然后退化成 `g.onTick = now => { if (now >= g.target) { g.onTick = null; g.done(); } }`。

判据是：把 rAF 或 `getContext('webgl')` 打成失败，主题仍应靠心跳走完并调用 `done()`，而不是让壳的超时来收尸。

---

## 7. 壳的闸门与抑制条件

写死在 `overlay.swift` 顶部，**主题改不了，flag 也调不高**：

| 常量 | 值 | 作用 |
|---|---|---|
| `maxVisibleSeconds` | 60 | 可见时长上限，从 `orderFront` 那刻算 |
| `maxProcessSeconds` | 150 | 进程总时长绝对兜底，防等待定时器本身出事 |
| `maxLeadSeconds` | 60 | 提前亮相上限 |
| `earlySlackSeconds` | 90 | 时间窗左边界余量 |
| `recheckInterval` | 5 | 亮相后复检全屏的间隔 |
| `heartbeatInterval` | 0.1 | 心跳节拍 |

Go 侧 `theme.TimeoutSeconds()` 与之对齐：主题自报的 `duration` 加 10 秒余量，但绝不超过 60。`duration` 缺省或非正时按 10 算。

### 时间窗是非对称的

```swift
delta >= -(lead + 90)  &&  delta <= grace
```

左边界要多放 90 秒，因为 launchd 只有分钟精度：向下取整到分钟意味着最坏情况会提前 `lead + 59` 秒被拉起来（见第 3 节）。
右边界是 `grace`（默认 1200 秒）。**没有这个判断，晚上十点开电脑会被祝贺下班**——launchd 对错过的时间点会在唤醒后补跑一次。

### 全屏抑制

`fullscreenAppInFront()` 只检查前台应用 layer 0 窗口的边界是否覆盖某块屏（2pt 容差），**不读窗口标题，不申请录屏权限**。
亮相后每 5 秒复检一次：用户可能中途开个全屏会议。

`--force`（即 `gong vis`）跳过时间窗和全屏两个判断，也不启动复检。

### 面板配置

```swift
panel.level = screenSaverWindow      // 能盖住别的 App 的全屏窗口
panel.ignoresMouseEvents = true      // 点击穿透
panel.collectionBehavior = [.canJoinAllSpaces, .stationary, .ignoresCycle, .fullScreenAuxiliary]
```

窗口类是 `OverlayPanel`，`canBecomeKey` / `canBecomeMain` 都返回 `false`，配合 `.nonactivatingPanel` 和 `NSApp.setActivationPolicy(.accessory)`：**不夺焦点、无 Dock 图标、不接管菜单栏**。展示用 `orderFrontRegardless()`，不是 `makeKeyAndOrderFront`。

WebView 的透明背景同时走公开 API 和 KVC（`underPageBackgroundColor` + `drawsBackground`），兼顾不同系统版本的行为差异。所以主题的 `html, body` 必须是 `background: transparent`。

`isInspectable = true`（macOS 13.3+），主题作者可以用 Safari 的开发菜单直接连上去调。

---

## 8. 主题目录与解析

```
~/.config/gong/themes/<id>/     用户主题，优先
<prefix>/share/gong/themes/<id>/  内置主题（Homebrew 布局）
./themes/<id>/                  开发布局
```

`paths.Builtin()` 依次尝试 ldflags 注入的路径、Homebrew 布局、可执行文件同级、当前目录。
Homebrew 的 `bin` 是软链接，所以 `exeDir()` 先 `EvalSymlinks` 再取目录，否则会在 `/opt/homebrew/bin` 旁边找 `../share`。

`theme.List()` 先加内置再加用户，同名时用户的覆盖内置的。`theme.Resolve()` 反过来，先查用户目录。

一个主题的最小构成是 `index.html`。`theme.toml` 缺了不算错，走默认值——只写 HTML 也该能跑。

```toml
lead      = 4      # 提前亮相秒数，0..60
duration  = 9      # 自报的演出时长，用来算 --timeout
author    = "..."  # 展示用；没有 @ 前缀会自动补
source    = "..."  # 只有 http/https 且带 host 才会被展示（SourceURL 校验）
```

`gong themes` 输出 `<id>  @author  <source>`，`source` 在 TTY 下加下划线（非 TTY 时不加，方便管道处理）。

两个保留名不能当主题 ID：`@random` 和 `@sequence`。另外目录名 `default` 会被 `List()` 跳过——它是 `led` 的历史别名，`config.NormalizeTheme` 负责把老配置里的 `"default"` 迁到 `"led"`。

---

## 9. 调试

```sh
gong vis <theme>          # 走真实渲染路径，只多一个 --force
tail -f "$TMPDIR/gong.err" # 主题的 console 和壳的报错都在这
gong ls                   # 看每条定时算出来的 launchd 唤醒时间
gong stop                 # pkill -x gong-overlay
```

直接跑壳（不经过 Go）：

```sh
./gong-overlay --force --timeout 3 --theme themes/led/index.html
```

Safari → 开发 → 你的机器名 → 能看到浮层里的 WebView（`isInspectable`）。

`console.log` 会同时进 Safari 控制台和 stderr，所以在没有截图权限的环境里，**把要验证的数值 log 出来读 stderr** 是可行的验证手段：字形掩码、逐帧统计、`gl.readPixels` 读回的像素颜色都能这么验。

---

## 10. 测试与 CI

`.github/workflows/ci.yml`（push 到 main + PR）：

```
go test ./...
go vet ./...
ruby -c packaging/gong.rb
swiftc -typecheck  (arm64 + x86_64)
```

Swift 只做 typecheck，不编译产物——`release.yml` 才会真编。

Go 测试覆盖的重点：

- `internal/agent` — 跨日偏移、分钟槽冲突、反查命中、legacy plist 清理
- `internal/config` — 解析、校验、原子保存
- `internal/theme` — 解析顺序、用户覆盖内置、`@random`/`@sequence` 的确定性
- `internal/theme/api_contract_test.go` — 读 `overlay.swift` 源码断言 Theme API v1 的字段和生命周期顺序
- `internal/tui` — 交互状态机

**主题本身没有自动化测试。** 视觉的东西没法这么验，靠 `gong vis` 加 stderr 日志人工看。发版前至少冒烟一个主题。

发版流程见 `AGENTS.md`。

---

## 11. 刻意不做的事

记在这里是为了避免有人「顺手补上」：

- **不申请录屏权限。** 全屏判断只用窗口边界，不读标题。多一个权限弹窗对一个下班提醒来说不值。
- **壳不读配置文件。** 所有决策在 Go 侧完成后内联成 flag。
- **主题之间不共享代码。** 复制粘贴是这里的正确做法，抽公共库不是。
- **定时没有名字。** 身份就是它在列表里的位置，`#N`。`label` 纯展示，可以为空、可以重复。
- **不为预览另写渲染路径。** `gong vis` 和真实触发共用一条。
- **`RunAtLoad` 永远是 `false`。** 装的时候不该立刻放一遍，登录时也不该跑任何东西。
