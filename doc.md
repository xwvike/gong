# gong（锣）—— 定时全屏浮层提醒 · macOS

> 到点在所有屏幕最顶层播一段 HTML 动画，宣告某个时刻到了，播完自动退出。
> 首个用例是下班提醒，但**不要按「下班」来设计**——TUI 要支持多条定时，名字和架构都按通用能力来。

## 一、当前状态

Swift 壳已重构完成并实测通过，两个主题跑通了完整契约。**代码就在本仓库**（不再是 `~/.offwork/`）。

| 项 | 结果 |
|---|---|
| 二进制大小 | 壳 124K（arm64 单架构）/ 268K（universal）；Go CLI 5.0M；release tarball 3.0M |
| 冷启动 | 时间窗拒绝路径 ~70ms；建 panel 到亮相 ~300ms |
| 出现 / 背景真透明 | ✅ 已验证 |
| 不夺焦点 / 点击穿透 | ✅ 已验证（浮层前后 `lsappinfo front` 不变） |
| 延迟亮相（lead） | ✅ 已验证 |
| 到点精度 | 壳按 target 敲，误差 <10ms |
| 可见 60s 闸门 | ✅ 已验证，失控主题实测 60.17s 被砍 |
| 稳定性 | nixie 连跑 8 次 6.61s（首次冷启 7.17s）；default 连跑 4 次 5.50s |
| 多显示器 | ⚠️ 未验证（无外接屏），代码是每块屏建一个 panel |
| 环境 | macOS 26.5.2 / Apple silicon / Swift 6.3.3 |

**已经发到 brew 上了**：`brew install xwvike/tap/gong` 装完即用。
仓库 github.com/xwvike/gong，tap github.com/xwvike/homebrew-tap（本来就有的那个，不要另建），v0.1.0。

实测过的分发链路：CI 出 universal binary（两个二进制都是 x86_64 + arm64 fat）→
release sha256 独立复核一致 → brew 装完 **没有 com.apple.quarantine**、
`Signature=adhoc, linker-signed` → 安装过程完全没碰 home →
从 Cellar 直接调 `gong on`，plist 里写的是 `/opt/homebrew/bin/gong`（不带版本号）。

未做：实际用一周、收摊脚本、用户退出入口、多屏实测。

## 一·五、分层（想清楚了再动手）

| 层 | 管什么 | 不管什么 |
|---|---|---|
| **壳** `gong-overlay` | 计时、拉起主题、**给主题提供能力**（暗亮模式、将来主题反过来跟壳通信） | 不读配置、不认识主题名、不知道有几条定时 |
| **Go** `gong` | 配置、TUI、launchd 接管 | 不参与渲染 |
| **主题** | 表现形式和文案 | 不做计时、不读参数 |

主题是**最重要也是能力最弱**的一层：它的表现力应该在设计上，不在程序上。所以壳给它的东西越少越好，凡是主题要自己写逻辑才能做到的事，都该考虑是不是壳该直接给。

「主题 → 壳」这条反向通道已经有雏形了：`done` 和 `log` 两个 messageHandler。将来加能力（暗亮模式、拿系统状态）就是往这条道上挂东西，不要另起炉灶。

## 二、硬约束（不能破坏）

1. **不夺焦点** —— 播放时用户正在打字，光标必须纹丝不动
2. **点击穿透** —— 鼠标当它不存在
3. **背景真透明** —— 盖在桌面上，不是盖住桌面
4. **常驻进程数为 0** —— 这是整个方案存在的理由
5. **能盖住其他 App 的全屏窗口**

## 三、为什么不是现成方案（别回退）

- **Clawd on Desk / Codex 宠物 / Anthropic claude-desktop-buddy**：全是 AI agent 状态镜像，事件源写死在 agent 生命周期里，没有通用事件入口。Clawd 是 Electron，常驻 150–250MB。
- **mpv `--fullscreen --ontop`**：会激活自己的 App，抢焦点。否决。
- **Chrome `--app --start-fullscreen`**：同样抢焦点，且做不了真透明。否决。
- **Tauri**：`alwaysOnTop` 只到 floating 层（3），盖不住别的 App 的全屏窗口；还要穿到 objc 改 window level 和激活策略。绕一圈还是写原生。

最终选型：**薄 Swift 壳 + WKWebView**。窗口语义用原生，动画用 HTML/CSS 写，改效果不用重新编译。

## 四、关键技术决策（踩过的，别推翻）

### 窗口层

- **`NSPanel` + `.nonactivatingPanel` + `canBecomeKey/Main = false`** —— macOS 上「盖在最上面但不参与焦点」的唯一正解。普通 `NSWindow` 做不到。
- **`NSApp.setActivationPolicy(.accessory)`** —— 无 Dock 图标、不接管菜单栏、启动不激活。
- **`orderFrontRegardless()`**，不是 `makeKeyAndOrderFront:`。
- **window level 必须是 `screenSaverWindow`（1000）** —— `.floating`（3）会被全屏 App 压在下面。
- **`web.setValue(false, forKey: "drawsBackground")`** —— 非公开 KVC 写法，但在 macOS 26 上仍有效、不抛异常。同时写公开的 `underPageBackgroundColor = .clear` 兜底。

### 时间与生命周期

- **时间窗判断不能删** —— launchd 的 `StartCalendarInterval` 在错过的时间点会在唤醒后补跑一次，没这个判断的话晚上十点开电脑会被祝贺下班。
- **窗口是非对称的** —— `[target - lead - 90s, target + grace]`。因为 launchd 只有分钟精度，lead=5 的定时实际是提前最多 60 秒被拉起来的。对称的 `abs()` 判断会把倒计时类主题全毙掉。
- **闸门必须用 `DispatchSourceTimer`，不能用 `asyncAfter`** —— 后者的 leeway 跟间隔成正比，实测 60 秒的闸门会飘到 63.17 秒。换成显式 `leeway: .milliseconds(50)` 后是 60.23 秒。闸门是给用户兜底的，不能由系统看着办。
- **reveal 和页面加载完成有竞态** —— lead=0 时 reveal 几乎立刻发生，那会儿注入的 `__gongReveal` 还不存在，`evaluateJavaScript` 喊了等于没喊，主题永远不启动、永远不喊 done，只能等兜底超时。所以 `reveal()` 和 `didFinish` 两边都要触发一次，靠 `revealed` 标志去重。**这个 bug 第一次实测就撞上了。**
- **`gong.now` 每次回调前必须刷新** —— 它是注入时写死的启动时刻，而页面是**提前加载**的。亮相可能发生在启动后 60 秒，主题在 `onReveal` 里拿 `gong.now` 当「现在」会差整整一个等待期。default 主题因此在真实路径下一亮相就判定「动画放完了」，立刻退出。
  所以 `__gongReveal(ms)` / `__gongFire(ms)` / `__gongTick(ms)` **都带时间戳**，进函数先写 `gong.now`。
  **这个 bug `--force` 测不出来**——force 的等待期是 0，误差正好为 0。教训：回归里必须有走 `--at` 的真实路径用例。

### 主题渲染（新踩的坑，最坑的一个）

**常驻 CSS 动画挂在带 `filter` 的元素上会把主线程压死，进而拖垮 JS 定时器。**

nixie 主题最初给 6 个带三层 `drop-shadow` 的数字各挂了一个 `opacity` 闪烁动画。实测：

| 配置 | 一个 2600ms 的 `setTimeout` 实际什么时候来 |
|---|---|
| 每个字各自跑 flicker | 2801ms / 6447ms / **超过 15 秒都不来** |
| flicker 改挂在容器 `.rig` 上 | 2604ms / 2605ms / 2604ms |

后果不是「动画卡一点」，是**主题永远不喊 done，浮层挂满 60 秒才被闸门砍掉**。空白页面上同样的定时器全部精确到 1ms，所以这不是 WebKit 对后台 App 的普遍节流，就是这个页面自己把主线程占死了。

规矩：**常驻动画只挂在没有 filter 的容器元素上，一个就够**。整排一起抖反而更像市电纹波。

但要注意：把动画收到容器上只是**减轻**，不是根治——收完之后仍然随机见过整轮定时器不来（同一份主题连续两次跑满 40s / 48s）。根治办法是主题里干脆不用 JS 定时器，改由壳喂心跳，见第七节「时间归壳，像素归主题」。

### 全屏检测

- **目前是启发式**（`visibleFrame.height >= frame.height`），开了「自动隐藏菜单栏」会误判。要更准就用 `CGWindowListCopyWindowInfo` 遍历 layer 0 窗口看 bounds——**只读 bounds，绝对不要读窗口标题**，读标题会触发屏幕录制权限申请，CLI 工具首次运行弹这个框体验极差。
- 亮相后每 5 秒复检一次：用户可能中途开个全屏会议，浮层在 screenSaver 层会盖在上面。

## 五、分发约束（会决定架构，写进 README）

**必须做成 Homebrew formula，永远不要做成 `.app` cask。**

- Homebrew 这轮 Gatekeeper 收紧**只影响 cask，不影响 formula**（源码构建和 bottle 都不影响）。Gatekeeper 弹窗只在文件带 `com.apple.quarantine` 时触发，Homebrew 不给 formula 打这个标记。
- 未签名未公证的 cask 已弃用，2026-09-01 从官方 tap 移除；`--no-quarantine` 逃生口也已废除。
- 「Apple Silicon 不允许未签名 arm64 代码执行」指的是**完全无签名**。linker 会自动打 ad-hoc 签名，实测产物是 `Signature=adhoc, linker-signed`，足够执行，**不需要 99 刀开发者账号**。

将来若有人提「能不能做成 App 拖进 Applications」，要能立刻拒绝。

### 走预编译 release，不走源码构建

GitHub Actions 出 universal binary，formula 指向 release tarball。理由：目标是**给别人用**，源码构建要拉 250M 的 Go 并要求装 Xcode CLT，对非开发者是致命摩擦。

```
swiftc -O -target arm64-apple-macos12  overlay.swift -o overlay-arm64
swiftc -O -target x86_64-apple-macos12 overlay.swift -o overlay-x86
lipo -create overlay-arm64 overlay-x86 -o gong-overlay
```

Go 侧 `GOARCH=arm64/amd64` 各来一份再 `lipo`。

顺带纠正一条旧结论：**别写 `depends_on xcode: :build`**。`swiftc` 在 Command Line Tools 里就有，那条依赖会逼用户装 15G 的完整 Xcode。走预编译之后连 CLT 都不需要。

### plist 不能由 brew 安装

三条硬理由：

1. formula 的 install 跑在沙箱里，只准写 Cellar 前缀，写 `~/Library/LaunchAgents` 会被拦；就算用 `post_install` 绕过去也违反 policy，而且 `brew install` 可能以别的用户身份跑。
2. bottle 是可重定位的 tar 包，倒 bottle 时不保证跑安装逻辑。
3. `brew services` 就是为填这个坑存在的，但它**一个 formula 只能有一个 service**，`cron` 字段只接受**一个** crontab 表达式，label 也被固定成 `homebrew.mxcl.gong`——表达不了「12:00 + 18:00 + 用户任意增删」。

所以 plist 由 `gong` 自己生成和 bootstrap。用户视角两步：

```bash
brew install xwvike/tap/gong
gong on
```

### formula 的布局（不需要注入任何东西）

```ruby
bin.install "gong"
bin.install "gong-overlay"
pkgshare.install "themes"      # → <prefix>/share/gong/themes
```

Go 侧按这个顺序找内置主题：ldflags 注入的 → `bin/../share/gong/themes` → 同级 `themes/`（开发用）。第二条正好是 Homebrew 的标准布局，**所以 formula 既不用 wrapper 脚本也不用 ldflags**。预编译产物本来也没法在安装时注入 ldflags。

### 生成 plist 的坑

- **二进制路径写 `bin` 不写 `Cellar`**：`/opt/homebrew/Cellar/gong/0.1.0/bin/gong` 在 `brew upgrade` 后就死了，而且是**静默失效**——每天到点什么都不发生，没有任何报错。所以 `paths.StablePath()` 会把含 `/Cellar/` 的路径改写回 `<prefix>/bin/`，再写进 plist。已实测：直接调 Cellar 里那个二进制，生成的 plist 里也是 `bin/gong`。
- **`launchctl load` 已废弃**，用 `launchctl bootstrap gui/$(id -u) <plist>` / `bootout gui/$(id -u)/<label>` / `kickstart -k` 调试。`bootstrap` 对已加载的 label 会报 `5: Input/output error`，所以 sync 时先 bootout（忽略错误）再 bootstrap。启停用 `launchctl disable/enable`，比删文件温和，配置还在。
- **内置主题留在 `pkgshare/themes`，不要 copy 到 home**，这样 `brew upgrade` 能更新它们。`~/.config/gong/themes/` 只放用户自己写的，解析时用户目录优先、回落到内置。
- **`brew uninstall` 不会清 plist**，formula 没有 uninstall hook。caveats 要写「卸载前先 `gong off`」，同时壳侧做到找不到主题就静默退出（已实现）。

## 六、命名与目录约定

```
仓库          github.com/xwvike/gong
tap           homebrew-tap        （已有仓库，gong 只是加一个 Formula/gong.rb）
CLI 二进制     gong          (Go + Bubble Tea)
渲染二进制     gong-overlay  (Swift)
配置          ~/.config/gong/config.toml
用户主题       ~/.config/gong/themes/<name>/{index.html,theme.toml}
内置主题       $(brew --prefix)/share/gong/themes/<name>/
launchd       ~/Library/LaunchAgents/local.gong.plist（所有定时共用一个）
```

命令形态（用子命令，不用 `-set` 这种单横线接单词）：

```bash
gong on                   # 写默认配置 + 生成 plist + bootstrap
gong off                  # bootout + 删 plist（卸载前跑）
gong set                  # TUI：定时列表 + 主题选择
gong vis <theme>          # 预览
gong ls                   # 列出已注册定时
gong rm <name>            # 删除某条定时
gong stop                 # 掐掉正在播的浮层（pkill -x gong-overlay）
```

默认两条定时：`noon` 12:00、`evening` 18:00，周一到周五。

## 七、架构要点

**不需要任何常驻进程。** launchd 本身就是那个常驻的东西，而且已经在跑了。TUI 按需拉起编辑完退出，`vis` 按需拉起播完退出。只有事件驱动触发（而非定时）才需要守护进程，当前不需要。

### 所有定时共用一个 plist，不是一条一个

**这条推翻了早先的写法**（早先是「每条定时一个独立 plist，label `local.gong.<name>`」）。

原因是 macOS 13 之后会把**每一个第三方 LaunchAgent** 单独列进「系统设置 → 通用 → 登录项与扩展 → 允许在后台」。一条定时一个 plist 就意味着 N 条定时在那里排出 N 个**一模一样的「gong」**：

```
Name: gong   Identifier: 8.local.gong.noon
Name: gong   Identifier: 8.local.gong.evening
```

用户既分不清谁是谁，还可能顺手关掉一个——**关掉之后我们完全无从知晓**，`gong ls` 照样显示已接管，那条提醒就静悄悄地不响了。

`StartCalendarInterval` 本来就是个数组，装多少个「星期几 + 几点几分」都行。所以现在是一个 label `local.gong`，把所有启用定时的触发点并进去，后台项永远只占一个。

两个连带好处：

- **同一分钟的触发点会去重**。以前两条定时撞在同一分钟，launchd 会叫醒两次，屏幕上叠两个浮层。
- plist 内容更简单，`gong fire` 也不带参数了。

代价是 launchd 不再告诉我们是哪条定时触发的，`gong fire` 得按当前时间反查最近的那条（`agent.Match`）。猜错也不至于出事——壳自己的时间窗判断会把不该放的挡掉。

**升级要清理**：0.1.0 装过的 `local.gong.<name>.plist` 会在 `gong on` 时被 bootout 并删除。扫描时**必须排掉 `local.gong.plist` 自己**——它也符合 `local.gong.*.plist` 这个形状，不排掉就会在写完新 plist 之后立刻把它当遗留物删了（写这段时踩了，已加回归测试）。

### launchd 调 `gong fire <name>`，不直接调 `gong-overlay`

这一条改了文档早先的写法（早先是「plist 里内联全部 flag，直接调壳」）。改的理由：

1. **收摊动作必须在 Go**（暂停音乐、`shortcuts run` 切专注、退 Xcode），那是任务清单第一条，早晚要有一个 Go 进程在触发链路上
2. 改主题、改文案不用重生成 plist —— 只有**时间或 lead 变了**才要（lead 决定 launchd 叫醒的那一分钟）
3. plist 内容变简单了，只剩 label + 时间 + `gong fire <name>`

代价是触发链路上多一个 Go 进程（约 10ms），但它 `syscall.Exec` 直接把自己换成壳，**不留多余进程，常驻进程数仍然是 0**。

### Go 侧结构

```
cmd/gong/main.go       子命令分发
internal/paths         所有磁盘位置 + 产物解析（含 StablePath）
internal/config        config.toml 读写、校验、时间解析
internal/theme         主题解析：用户目录优先、回落内置
internal/agent         plist 生成 + launchctl + Sync
internal/tui           gong set
```

`agent.Sync` 是幂等的：enabled 的装上，disabled 和配置里已删除的卸掉。所以 `gong on` 可以随便重复跑。

### 职责划分（重要）

所有配置逻辑留在 Go，**Swift 壳保持无状态、不读任何配置文件**。Go 读主题的 `theme.toml`，把参数全部内联成 flag：

```
gong-overlay --at 12:00:00 --lead 5 --grace 1200 --timeout 20 \
             --name noon \
             --theme ~/.config/gong/themes/nixie/index.html
```

**传进去的 flag 全是「什么时候」和「活多久」，没有一个是「长什么样」。**

提醒的形式和文案完全由主题决定，不通过启动参数传。曾经有过一个 `--message`，已经删掉了——它诱导人去写「一套动画 + 十种文案」，而真正决定提醒有没有效果的是**形式本身**（倒计时、闸门砸下、绕圈的猫），不是那行字。要不同的提醒就写不同的主题，复制一份改几行 HTML 的成本本来就该很低。

同理 `--name` 只用来给 stderr 打标，**不注入页面**——注进去只会诱导主题写 `if (name === 'noon')`，等于把表现逻辑漏回配置层。

### lead：launchd 没有秒

`StartCalendarInterval` 只有 `Hour`/`Minute`，**没有 `Second`**。所以「11:59:55 开始倒数、12:00:00 到点」这种效果不可能由 launchd 直接触发。

正确形态：**Go 把 plist 触发点挪到 `at - lead` 所在的那一分钟，壳先把页面加载好但不亮相，等到 `at - lead` 才 `orderFront`。**

```
11:59:00.0   launchd 拉起进程
11:59:00.3   时间窗判断通过，建 panel + 加载 WebView，【不 orderFront】——屏幕上什么都没有
             ... 静躺 55 秒，几乎零 CPU ...
11:59:55.0   orderFront，页面瞬间可见（已经加载好了，首帧零延迟）
12:00:00.0   window.gong.target 到点，主题自己动作
12:00:04.0   主题喊 done，进程退出
```

「提前加载、延后亮相」是白赚的：WebView 那 200ms 加载和首帧编译在等待期就做完了，不会先闪一下白底。

**lead 是主题的属性，不是定时的属性。** 用户在 TUI 里设的是「12:00」，他不该知道辉光管需要提前 5 秒。换个主题，plist 自动重算。

**已知边界情况**：11:59:30 合盖，进程被挂起，12:00 那一刻没发生；醒来后 launchd 也不会补跑，因为 11:59:00 那个触发点它已经跑过了。这一次提醒就是丢了。lead 越小窗口越窄，所以**约定 lead 上限 60 秒**（壳里 clamp 了）。

### 两道闸门

- **可见 ≤ 60s**，从 `orderFront` 那一刻算，写死在 Swift 里，`--timeout` 只能往下调不能往上
- **进程 ≤ 150s** 绝对兜底，防等待定时器本身出事

60 秒这个上限挂在**可见时长**上而不是进程时长上，因为提前亮相方案下进程会先静躺最多 60 秒，那段时间屏幕上什么都没有，不该计入。

### 主题就是目录

`themes/<name>/{index.html, theme.toml}`。`gong vis` 和真实触发走**完全同一条渲染路径**，只是 `--force` 跳过时间窗和全屏判断。主题作者只写 HTML/CSS，零 Swift 知识，这是这个架构最值钱的地方。

**不要为预览另写一套浏览器 harness**——那就变成两条路径了，两条路径迟早会不一致。调试就用 `./gong-overlay --force`，壳已经把 `console.log` 和未捕获异常桥到 stderr 了。

`theme.toml`：

```toml
name      = "辉光管"
desc      = "六管辉光管时钟悬在屏幕正中，只留灯丝。"
lead      = 5        # 提前多少秒亮相
duration  = 10       # 预期可见时长（秒），Go 拿它算 --timeout
placement = "center" # center / edge / corner，给 TUI 提示会不会挡住视线
webgl     = false
```

### 主题契约：window.gong

壳用 `WKUserScript` 在 `.atDocumentStart` 注入。**壳只给「时间」和「几何」，其余全归主题。**

```js
window.gong = {
  target:  1753588800000,    // 目标时刻绝对毫秒
  now:     1753588795012,    // 壳那边的当前时间，每次心跳刷新
  lead:    5,
  force:   false,
  revealed: false,
  fired:    false,
  screens: 1,
  screen:  { index: 0, isMain: true, w: 1512, h: 982, scale: 2 },

  onReveal: undefined,       // 壳亮相时调（target - lead）
  onTick:   undefined,       // 壳每 100ms 调一次，参数是当前毫秒
  onFire:   undefined,       // 壳在 target 那一刻精确调一次
  done:     function () {}   // 主题调它通知壳退出
}
```

对应地 `<html>` 上会依次出现 `gong-live`（亮相）和 `gong-fired`（到点）两个 class，纯 CSS 主题可以什么 JS 都不写。

五条规矩：

1. **动画必须挂在 `html.gong-live` 下面。** 页面是被壳提前加载好的，按 `load` 起算会跑偏几十秒。
2. **`target` 是绝对时间戳。** 主题不用关心自己是提前 5 秒还是 60 秒被亮出来的。
3. **主题里不要写 `setTimeout` / `setInterval` / `requestAnimationFrame` 来卡点。** 用 `onTick` / `onFire`，理由见下。
4. **多屏时每块屏是一个互不知情的 WebView 实例。** 任何一个喊 done 都会带走整个进程，所以壳里只认 `screen.index === 0` 那一份，其余的调了直接 return。要判断「这块屏该不该画」用 `gong.screen`。
5. **常驻 CSS 动画不要挂在带 `filter` 的元素上**，见第四节。

### 时间归壳，像素归主题

页面里的 JS 定时器**不可靠**，这是实测结论，不是洁癖：

| 环境 | 一个 2600ms 的 `setTimeout` 实际什么时候来 |
|---|---|
| 空白页面 | 2601ms（精确到 1ms） |
| nixie（每个字各跑 filter 动画） | 2801ms / 6447ms / **超过 15 秒都不来** |
| nixie（动画收到容器上之后） | 2604ms，但仍见过整轮不来 |

而壳这边的 `DispatchSourceTimer` 误差只有几毫秒。所以**时间由壳来敲**：

- 壳每 100ms 调一次 `__gongTick(now)`
- 壳在 `target` 那一刻调一次 `__gongFire()`
- 主题所有的计时都换算成「心跳来了几次」，一个页面定时器都不留

改完之后 nixie 连跑 8 次全部 6.61s（首次冷启 7.17s），default 连跑 4 次全部 5.50s。改之前同样的配置会随机跑出 40s / 48s——那不是「动画卡一点」，是主题永远喊不出 done、浮层挂满 60 秒才被闸门砍。

代价是主题的计时精度等于心跳粒度（100ms），对这个场景绰绰有余。

## 八、任务清单（建议顺序）

- [x] Swift 壳重构：flag 解析、延迟亮相、`window.gong` 注入、非对称时间窗、全屏复检、双闸门、日志桥、心跳
- [x] 两个主题跑通契约：`default`（闸门）、`nixie`（辉光管）
- [x] Go CLI：`on/off/set/ls/rm/vis/stop/fire/themes`，配置、主题解析、plist 生成、launchctl 接管
- [x] Bubble Tea TUI：增删改查、启停、换主题、预览
- [x] CI 预编译 universal binary + Homebrew formula
- [x] 开仓库、发 v0.1.0、formula 进 tap，`brew install xwvike/tap/gong` 已验证可用
- [ ] **装 launchd，实际用一周**，确认自己还想要它
- [ ] **写「收摊」脚本并接进触发流程**（挂在 `cmd/gong/main.go` 的 `cmdFire` 里，已经留好位置）。纯动画的提醒半个月后一定会被无视，真正改变行为的是替用户收摊：
  ```bash
  osascript -e 'tell application "Music" to pause'
  shortcuts run "下班"          # 快捷指令切专注模式，macOS 上唯一稳定的切 Focus 办法
  osascript -e 'quit app "Xcode"'
  ```
  `shortcuts run` 需要用户先在快捷指令 App 里建好动作，命令行只负责触发。
- [ ] **用户退出入口**（推迟到现有功能玩完再做）
- [ ] 多显示器实测（没有外接屏，一直没验过）

### 发版怎么走

```bash
git tag v0.1.0 && git push origin v0.1.0     # CI 自动出 universal tarball 和 sha256
# 把 packaging/gong.rb 拷到 tap 仓库 Formula/gong.rb，填上 release 里的 sha256
brew install xwvike/tap/gong && gong on
```

tap 用的是**已有的** `xwvike/homebrew-tap`（brew 里叫 `xwvike/tap`），gong 只是往里加一个 `Formula/gong.rb`，
跟里面 goreleaser 自动维护的 `Casks/local-mirror.rb` 互不干扰。

### 关于退出入口（推迟，但结论先记着）

当前唯一的逃生口是 60 秒可见闸门和 `pkill`。将来要做用户主动退出，Esc 这条路的结论是：

- 壳**结构上收不到键盘事件**：`canBecomeKey = false` 意味着永远不是 key window，`addLocalMonitorForEvents` 一个事件都拿不到。
- 全局监听有权限不对称：`addGlobalMonitorForEvents(.keyDown)` 要「输入监控」TCC 授权，`CGEventTap` 要辅助功能授权，**但鼠标事件的全局监听不要权限**。
- **`RegisterEventHotKey`（Carbon）注册系统级热键不需要任何 TCC 授权**，对 `.accessory` 且从不激活的进程照样有效。这是唯一的正路，Apple 至今没给替代品也没弃用。
- 代价：注册期间 Esc 会被吞掉，底下的 App 收不到。所以**只在浮层可见期间注册，退出立刻 `UnregisterEventHotKey`**。
- **未实测**：无修饰键单独注册 Esc（modifiers 传 0）在 macOS 26 上是否会被拒。要做的时候第一件事是验这个，被拒就退到 `⌘.`。
- 退出走优雅路径：先调 `gong.onDismiss?.()` 给主题 250ms 收尾，同时挂硬兜底强杀。

## 九、现有代码

代码就在仓库里，**不要再往这份文档里内联 Swift/HTML 源码**——上一版这么干过，结果文档里的 swift 和磁盘上的 swift 悄悄分了叉。

```
overlay.swift          Swift 壳
themes/default/        闸门主题（原 index.html）
themes/nixie/          辉光管主题
```

编译与自测：

```bash
swiftc -O overlay.swift -o gong-overlay

./gong-overlay --force --theme themes/default/index.html
./gong-overlay --force --lead 5 --theme themes/nixie/index.html
./gong-overlay --at 12:00:00 --lead 5 --grace 1200 --timeout 20 \
               --name noon --theme themes/nixie/index.html
```

`--force` 跳过时间窗和全屏判断，是 `gong vis` 的雏形。主题里的 `console.log` 和未捕获异常会打到 stderr，前缀 `gong-overlay[js]`。

---

## 十、发版踩的坑

- **`.gitignore` 的模式不带前导 `/` 会匹配任意层级。** 写了 `gong` 想忽略根目录的编译产物，结果把 `cmd/gong/` 整个目录也忽略了，初版提交里没有 Go 的 main 包，CI 在 `go build ./cmd/gong` 直接挂。产物模式一律写成 `/gong` 这种锚定形式。
  更值得记的是**怎么发现的**：`git add -A` 之后我打印了 staged 列表却没逐项核对。可靠的做法是拿 `git ls-files` 跟磁盘 `find` 做 `comm -23` 差集，空集才算齐。
- **CI 的 Go 版本别写死**，用 `go-version-file: go.mod`，否则 `go.mod` 一升版就挂。
- **release 的 sha256 要自己下载复核一遍**，不要直接抄 CI 输出的那行——formula 里填错 sha 的表现是所有人都装不上。
- **发版前先看看仓库里已经有什么。** 我照 doc 里那句「tap `homebrew-gong`」直接建了个新 tap，其实 `xwvike/homebrew-tap` 早就存在（goreleaser 在维护 local-mirror 的 cask）。而 doc 当时是**自相矛盾**的——第六节写 tap 叫 `homebrew-gong`，安装命令却写着 `xwvike/tap/gong`；我把矛盾按「改命令迁就仓库名」解决了，正好改反。
  文档内部打架时，**去查现实**（`gh repo list`）再决定听谁的，不要挑一个顺手的。
  顺带：`xwvike/tap` 这种通用 tap 名也比一个软件一个 tap 好——多个软件共用一个 tap 是 Homebrew 的常规做法。
