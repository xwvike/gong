# gong（锣）—— 定时全屏浮层提醒 · macOS

> 到点在所有屏幕最顶层播一段 HTML 动画，宣告某个时刻到了，播完自动退出。
> 首个用例是下班提醒，但**不要按「下班」来设计**——TUI 要支持多条定时，名字和架构都按通用能力来。

## 一、当前状态

Swift 壳已重构完成并实测通过，`default`（LED 信号板）和 `tunnel`（字幕隧道）两个主题都跑通了完整契约。**代码就在本仓库**（不再是 `~/.offwork/`）。

| 项 | 结果 |
|---|---|
| 二进制大小 | 壳 124K（arm64 单架构）/ 268K（universal）；Go CLI 5.0M；release tarball 3.0M |
| 冷启动 | 时间窗拒绝路径 ~70ms；建 panel 到亮相 ~300ms |
| 出现 / 背景真透明 | ✅ 已验证 |
| 不夺焦点 / 点击穿透 | ✅ 已验证（浮层前后 `lsappinfo front` 不变） |
| 延迟亮相（lead） | ✅ 已验证 |
| 到点精度 | 页面已加载且系统未休眠时，壳按 target 敲，实测误差 <10ms；迟到或晚加载走补发语义 |
| 可见 60s 闸门 | ✅ 已验证，失控主题实测 60.17s 被砍 |
| 稳定性 | default（LED 信号板）连跑 3 次 7.25s / 7.25s / 7.26s |
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
| **壳** `gong-overlay` | 原生窗口、计时、屏幕快照、Theme API v1、`done`/日志桥 | 不读配置、不认识主题目录 ID、不知道有几条定时 |
| **Go** `gong` | 配置、TUI、主题元数据、调度参数、launchd 接管 | 不参与渲染、不直接注入 HTML |
| **主题** | 表现形式和文案，按宿主绝对时间推进视觉 | 不读 Go 配置、不改宿主状态、不自行调度目标时刻 |

主题是**最重要也是能力最弱**的一层：它的表现力应该在设计上，不在程序上。所以壳给它的东西越少越好，凡是主题要自己写逻辑才能做到的事，都该考虑是不是壳该直接给。

「主题 → 壳」这条反向通道已经固定为 `done` 和内部日志桥。将来若要加入暗亮模式或系统状态，必须按 Theme API 的版本规则发布新契约，不能直接给 v1 偷加字段。

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
- **reveal 和页面加载完成有竞态** —— lead=0 时 reveal 几乎立刻发生，那会儿注入的 `__gongReveal` 还不存在，`evaluateJavaScript` 喊了等于没喊，主题永远不启动、永远不喊 done，只能等兜底超时。所以 `reveal()` 和 `didFinish` 两边都要触发一次，靠宿主状态和文档内私有 `didReveal` 标志去重。**这个 bug 第一次实测就撞上了。**
- **`gong.now` 每次回调前必须刷新** —— 它是注入时写死的启动时刻，而页面是**提前加载**的。亮相可能发生在启动后 60 秒，主题在 `onReveal` 里拿 `gong.now` 当「现在」会差整整一个等待期。default 主题因此在真实路径下一亮相就判定「动画放完了」，立刻退出。
  所以私有桥接函数 `__gongReveal(ms)` / `__gongFire(ms)` / `__gongTick(ms)` **都带时间戳**，进函数先写 `gong.now`；公开回调仍固定为无参 `onReveal()`、无参 `onFire()` 和单参数 `onTick(now)`。
  **这个 bug `--force` 测不出来**——force 的等待期是 0，误差正好为 0。教训：回归里必须有走 `--at` 的真实路径用例。

### 主题渲染（新踩的坑，最坑的一个）

**常驻 CSS 动画挂在带 `filter` 的元素上会把主线程压死，进而拖垮 JS 定时器。**

旧版 nixie 辉光效果最初给 6 个带三层 `drop-shadow` 的数字各挂了一个 `opacity` 闪烁动画。实测：

| 配置 | 一个 2600ms 的 `setTimeout` 实际什么时候来 |
|---|---|
| 每个字各自跑 flicker | 2801ms / 6447ms / **超过 15 秒都不来** |
| flicker 改挂在容器 `.rig` 上 | 2604ms / 2605ms / 2604ms |

后果不是「动画卡一点」，是**主题永远不喊 done，浮层挂满 60 秒才被闸门砍掉**。空白页面上同样的定时器全部精确到 1ms，所以这不是 WebKit 对后台 App 的普遍节流，就是这个页面自己把主线程占死了。

规矩：**常驻动画只挂在没有 filter 的容器元素上，一个就够**。整排一起抖反而更像市电纹波。

但要注意：把动画收到容器上只是**减轻**，不是根治——收完之后仍然随机见过整轮定时器不来（同一份主题连续两次跑满 40s / 48s）。根治办法是主题里干脆不用 JS 定时器，改由壳喂心跳，见第七节「时间归壳，像素归主题」。

### 页面里的自有时钟（这一节的结论被复测推翻过一次）

上面把定时器失灵归因于「重绘压力把主线程压死」。写 `noise` 主题时插桩量了一次，归因改成了**遮挡节流**，并记下这张表（5 秒观测窗口）：

| 时钟 | 期望次数 | 当时实测 | **2026-08-05 复测** |
|---|---|---|---|
| `requestAnimationFrame` | ~300 | 1（只有初始化那次） | **374** |
| `setInterval(16ms)` | ~300 | 5 | **251** |
| `setTimeout(16ms)` 自链 | ~300 | 5 | **252** |
| 壳的 `evaluateJavaScript` 心跳 | 50 | 50，分毫不差 | **50，分毫不差** |

**这张表右边一列推翻了左边那列。** 写 `dotcut` 时按「rAF 不跑」设计，做完顺手复测，发现页面本地时钟是活的：rAF 折合 **74.8fps**。排除过两个混淆项——页面里有没有常驻 CSS 动画结果一样（380 帧），rAF 回调里跑满 dotcut 的真实负载（924 个圆盘+内环攒一条 Path2D 走 evenodd）也一样（381 帧），**不是空转才快**。

旧数据是怎么来的、是 macOS 版本变了还是壳后来改动的副作用，没有回溯，所以**只把结论标成过期，不删除**。写主题前请以复测那列为准，并且注意复测走的是 `--force`（`gong vis`）路径。

三个后果：

1. **画面时钟用 rAF，绝对时刻用 `gong.now`。** 心跳 10Hz 只够定「到没到点」，拿它当画面时钟整场都在抖。正确写法是每次 `onTick` 打一个锚点（壳的绝对毫秒 ↔ 当时的 `performance.now`），两帧心跳之间由 `performance.now` 从锚点往前推——画面按刷新率走，绝对时刻每 100ms 被校正一次，不累计漂移。`dotcut` 就是这么写的，实测 **85fps**。
2. **但退出闸门和场景推进必须同时挂在 `onTick` 上。** 既然这条结论历史上翻过一次，就不能假设 rAF 在所有机器/系统版本上都活着。`dotcut` 的做法是让心跳和 rAF 调同一个 `update(now)`，而 `done()` 只由心跳发；实测把 `requestAnimationFrame` 换成空函数后，主题退化成 10.1fps 并照常在 9.34s 正常退出，不会挂到兜底 timeout。第七节那条「不要拿 rAF 当业务时钟或退出闸门」依然成立。
3. **CSS 过渡/动画不受影响**，它们由合成器驱动，不走页面的 JS 调度器。`tunnel` 整套效果都是 CSS transition，所以它一直是好的。

仓库里三个旧主题按这条复测重新过了一遍：

| 主题 | 画面怎么驱动 | 复测结果 |
|---|---|---|
| `default` | rAF 画滚动，`gong.now` 管收尾 | **已自愈**。曾记「从来没滚动过、`累计rAF帧数=0`」，现测 527 帧 / 7.06s = **74.7fps**，7 秒滚过 76.4 列（总 113 列）。架构本来就是对的，一行没改 |
| `noise` | 曾只在 `onTick` 里逐行 `drawImage` | **已改成锚点 + rAF**，实测 **85.3fps**（每帧 603 行 drawImage、约 5 万次/秒，扛得住）。撕裂从每秒换 10 次图案变成 75 次，更接近真实模拟信号雪花的场频；收敛曲线仍由绝对时间驱动，节奏一点没变 |
| `tunnel` | 纯 CSS transition，`onTick` 只切离散状态 | 无影响，一直是满帧 |

`default` 那条缺陷记录到此关闭。

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
gong ls                   # 列出已注册定时，最左一列的序号就是 rm 用的那个
gong rm <序号>            # 删除某条定时（序号看 gong ls，不是名字）
gong stop                 # 掐掉正在播的浮层（pkill -x gong-overlay）
gong uninstall            # 一条龙卸干净，最后自动 exec 到 brew uninstall
```

`off` 和 `uninstall` 是两件事：`off` 只从 launchd 撤出、程序和配置都留着（想暂停一阵子）；
`uninstall` 是不要了。

**为什么卸载要单独做一条命令**：formula 没有 uninstall hook，光在 caveats 里写「卸载前先跑 gong off」
是不够的——没人看 caveats，留下的 plist 会每天到点去拉一个不存在的二进制，而且是静默失败。
把清理和卸载合成一条，用户就没机会漏掉前半截。

最后一步用 `syscall.Exec` 换成 brew：进程映像已经变成 brew 了，它接下来删掉
`/opt/homebrew/bin/gong` 完全没有「删正在跑的文件」这个问题。

判断是不是 brew 装的，看**可执行文件在不在 `brew --prefix` 底下**，别去问 `brew list`
——源码编译的和 brew 装的可能同时存在，问 `brew list` 会把源码跑的那个也误判成 brew 装的。

默认两条定时：#1 12:00（标签「午间」）、#2 18:00（标签「下班」），周一到周五。

### 定时不需要名字，身份是它的位置

**这条推翻了早先的写法**（早先 `Schedule.Name` 是必填、唯一、限字符的标识符，`gong rm <name>` 靠它定位）。

现在 `Schedule` 只有一个可选的 `Label`——纯装饰，留空、跟别的定时重复、带空格，一律合法。真正的身份是它在 `c.Schedules` 里的**位置**：`gong ls`/TUI 表格里的 `#1`/`#2` 就是这个位置的 1-based 显示，`gong rm <序号>` 也按这个来。

推翻的理由很直接：起名字是一道没必要的门槛。用户要的往往是「中午一条、下班一条」，不是给每条定时想一个还要保证不重复、不能带空格的标识符。序号天然满足身份的两个要求——**存在、可辨认**——不需要用户多做任何事。真想要「午间」「下班」这种可读的标签，加一个纯展示、没有任何约束的字段就够了。

**已经确认过 Name/Label 从来不是系统集成层面的标识符**（这是能放心做这次改动的前提）：`paths.Label`（launchd job 名）在 v0.1.1 合并单 plist 时就已经是全局常量 `local.gong`，跟某条定时的名字毫无关系；`LegacyLabel(name)` 只在扫描磁盘上 0.1.0 遗留的 `local.gong.<name>.plist` 文件名时才用得到那个 `name`，而那是从**文件名**解析出来的字符串，不是从 `config.Schedule.Name` 读出来的——所以去掉 `Name` 字段，一行迁移逻辑都不用碰。

具体改动：

- `config.Schedule.Label string`，无校验；`Validate()` 不再检查名字非空/唯一/字符集
- `Schedule.Ref(i int) string`——有标签用标签，没有就是 `"#" + (i+1)`，所有面向用户的消息（删除确认、主题解析失败告警、`gong on` 装完的摘要）统一走这个
- `config.Config.At(i)` / `RemoveAt(i)` 按位置存取，取代原来按名字查找的 `Find`/`Remove`
- `gong fire` 那条「带名字调用只为兼容 0.1.0」的分支整个删掉了：Name 已经不存在，任何位置参数一律忽略、永远按时间反查——旧 plist 传进来的参数变成噪音，反查本来就能算出同一个绝对目标时刻，效果等价
- `agent.SyncResult.Active` 从 `[]string`（名字）改成 `[]agent.ActiveSchedule{Index, Schedule}`——它是 `Sync()` 内部过滤（只留启用且主题有效的）之后的子集，下标从 0 重新数，跟原始位置对不上，所以必须把原始 `Index` 一起带出来，不能只传 `config.Schedule` 指望调用方自己算——这个坑在写 `install()` 的打印逻辑时就地撞上了，修完才定型成现在这样

**旧配置文件怎么办**：0.1.x 写的 `config.toml` 里有 `name = "noon"` 这一行，没有 `label`。TOML 解码器会安静地忽略未识别的 `name` 字段，`Label` 落回空字符串——不报错、不崩，只是那条定时从此显示成 `#1` 而不是 `noon`，功能一点没丢。`gong set` 里随手按 `r` 就能把 "noon" 重新填回 Label（或者干脆不填，让编号说话）。

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

### TUI 组件化重写（`gong set`）

第一版 `gong set` 是手写 `fmt.Sprintf` 拼字符串、手写输入缓冲、手写按键提示——能用，但谈不上"像样"。改用 `bubbles` 的现成组件后分工是：

| 组件 | 用在哪 | 为什么 |
|---|---|---|
| `table` | 定时列表 | 列对齐、光标、滚动不用自己算；而且它按 `runewidth` 算宽度，中文字符正确按双宽处理——原来手写的 `%-12s` 是按 rune 数对齐，中文名字/星期会错位 |
| `list` | 主题库（新 tab） | 自带 `/` 模糊搜索、分页、状态栏；主题多了之后靠名字翻已经不够用 |
| `textinput` | 编辑标签 | 光标和编辑键位交给它；标签是自由文本，没有校验 |
| `help` + `key.Binding` | 底部按键提示 | 按 `?` 能展开全帮助，不用手写两份文案 |

新增一个 tab 切换：`定时` / `主题库`。编辑一条定时时按 `t` 能跳进主题库整个逛（带描述、带搜索），选中回车就带着选择回到编辑态，不用再靠左右键盲猜主题名循环。

顺带把 `Grace`（宽限）字段第一次暴露到 TUI 里——原来这个字段只能手改 `config.toml`，编辑面板里根本没有对应的字段。

**两个组件都有别处不会写文档告诉你的坑**：

1. **`table` 的 `renderRow` 用 `runewidth.Truncate` 截断单元格，这个函数不认 ANSI 转义。** 谁敢往一个单元格字符串里塞 `lipgloss` 的颜色，截断逻辑会把转义序列当成普通字符去数宽度、去切，输出直接花掉。所以表格单元格只能是纯文本，颜色只能加在 `Header`/`Selected` 这种整行样式上。主题解析失败的定时，没有在单元格里标红或加 `!` 后缀，而是走 `View()` 里单独一行的告警——`list` 那边不受这个限制，它的 `DefaultDelegate.Render` 用的是 ANSI-safe 的 `ansi.Truncate`，所以主题库的描述文字可以放心嵌颜色。
2. **`table.SetHeight(h)` 内部会先减掉表头那一行才是真正的可见数据行数**（`viewport.Height = h - lipgloss.Height(headersView())`），传行数本身会少显示最后一行——两条定时只显示了一条，排查了好一阵才在源码里翻到这行减法。`list.SetSize` 同样有这个类型的坑：它的内容区是 `lipgloss.NewStyle().Height(availHeight).Render(...)` 硬性撑开的，条目不够多就会拿空行填满剩下的高度，不会像 flex 布局那样自动收缩。两处都改成了按实际行数/条目数动态算高度（封顶在终端可用空间内），不再无脑塞满整个屏幕。

**这些坑不是读代码看出来的，是把渲染结果肉眼过了一遍才发现的**——`go build`/`go vet`/单测全绿的时候，界面上一大截空白和 `12: 00 :00` 这种错位一个都不会报错。做法是绕开 `teatest`（它只暴露持续的字节流，不是"当前屏幕"快照，硬要拿它当截图工具会很别扭），直接对 `Model` 连续调用 `Update`/`View`（纯函数，不需要真终端），在每一步之间把 ANSI 转义剥掉打印出来，肉眼扫一遍。改完效果、验完就删，不留在正式测试里——它不断言任何东西，留着只会腐烂。

行为覆盖走的是 `charmbracelet/x/exp/teatest`：起一个真正的 `tea.Program`，用 `tm.Send(tea.KeyMsg{...})` 模拟按键，`tm.FinalModel(t)` 拿到退出时的模型状态断言字段。这条路能测「按两次 q 才退出」「保存校验失败不退出」这种真实涉及 `tea.Quit` 时序的行为，光靠直接调 `Update` 测不出来（因为 `tea.Quit` 只是个 cmd，得有真正的 `Program` 在跑才会真的终止）。

### 职责划分（重要）

所有配置逻辑留在 Go，**Swift 壳保持无状态、不读任何配置文件**。Go 读主题的 `theme.toml`，把参数全部内联成 flag：

```
gong-overlay --at 12:00:00 --target 1753588800 --lead 5 --grace 1200 --timeout 20 \
             --tag '#1' \
             --theme ~/.config/gong/themes/default/index.html
```

`--target` 是这次触发对应的 Unix 秒，优先于只按当天解析的兼容参数 `--at`；Swift 转成 Theme API 时再变为 Unix 毫秒。

**传进去的 flag 全是「什么时候」和「活多久」，没有一个是「长什么样」。**

提醒的形式和文案完全由主题决定，不通过启动参数传。曾经有过一个 `--message`，已经删掉了——它诱导人去写「一套动画 + 十种文案」，而真正决定提醒有没有效果的是**形式本身**（倒计时、闸门砸下、绕圈的猫），不是那行字。要不同的提醒就写不同的主题，复制一份改几行 HTML 的成本本来就该很低。

同理 `--tag` 只用来给 stderr 打标，**不注入页面**——注进去只会诱导主题写 `if (tag === '#1')`，等于把表现逻辑漏回配置层。

它早先叫 `--name`，跟着「定时有名字」那版设计一起改掉了：现在定时的身份是序号，Go 传进来的是 `#1` 或 `vis`，纯粹为了在 `/tmp/gong.err` 里认出是哪次触发。

`--theme` 是必填的，缺了直接以退出码 2 报错。壳不猜路径——它不读配置，也不知道内置主题装在 brew 前缀的哪个位置（那是 Go 侧 ldflags 和回落逻辑的事）。曾经有过一段「缺 `--theme` 就试 `~/.config/gong/themes/default/index.html`」的兜底，既写死了一个主题名，又只猜用户目录，brew 装的内置主题根本不在那儿，结果是把「参数没传」换成了一条更难懂的 `theme not found`。

### lead：launchd 没有秒

`StartCalendarInterval` 只有 `Hour`/`Minute`，**没有 `Second`**。所以「11:59:55 开始倒数、12:00:00 到点」这种效果不可能由 launchd 直接触发。

正确形态：**Go 把 plist 触发点挪到 `at - lead` 所在的那一分钟，壳先把页面加载好但不亮相，等到 `at - lead` 才 `orderFront`。**

```
11:59:00.0   launchd 拉起进程
11:59:00.3   时间窗判断通过，建 panel + 加载 WebView，【不 orderFront】——屏幕上什么都没有
             ... 静躺 55 秒，几乎零 CPU ...
11:59:55.0   orderFront，页面瞬间可见（已经加载好了，首帧零延迟）
12:00:00.0   外壳置 fired、添加 gong-fired，并调用主题的 onFire()
12:00:04.0   主题喊 done，进程退出
```

「提前加载、延后亮相」是白赚的：WebView 那 200ms 加载和首帧编译在等待期就做完了，不会先闪一下白底。

**lead 是主题的属性，不是定时的属性。** 用户在 TUI 里设的是「12:00」，他不该知道白炽灯丝主题需要提前 5 秒。换个主题，plist 自动重算。

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
name      = "白炽灯丝"
desc      = "六位钨丝时钟由暗红升至暖白，到点后留下冷却余辉。"
lead      = 5        # 提前多少秒亮相
duration  = 10       # 从实际亮相到视觉预计结束的总秒数，包含 lead
placement = "center" # center / edge / corner，给 TUI 提示会不会挡住视线
webgl     = true      # 仅声明主题会自行尝试 WebGL；Go/外壳不据此改变行为
```

`name`、`desc`、`lead`、`duration` 和 `placement` 由 Go 读取；`webgl` 当前只是声明性元数据，没有运行时消费者。无论真假，它都不会允许、禁止或替主题开启 WebGL，也不会传进 `window.gong`。`duration` 表示从 reveal 到视觉预计结束（以及主动主题通常调用 `done()`）的总秒数，纯 CSS 主题也照此填写；它不传给主题。Go 将外壳兜底时间算成 `min(duration + 10, 60)` 秒，并从实际亮相时刻开始计时。只有 `lead` 会同时参与调度并出现在 Theme API 中。

`theme.toml` 可以省略；TOML 语法和已知字段类型仍须能被 Go 解析，但当前不校验必填字段、`placement` 枚举或未知键。缺省时展示名回退到目录 ID，`desc`/`placement` 为空、`lead = 0`、`webgl = false`；`duration <= 0` 按 10 秒计算。Go 会把 `lead` 钳在 `0..60`，最终 timeout 不超过 60 秒。

三层之间的数据边界是固定的：

| 层 | 输入 | 输出 / 对下一层的影响 |
|---|---|---|
| Go CLI | 定时配置、主题目录、`theme.toml` | 计算目标时刻、`lead` 和 timeout，以参数启动 Swift；传入 HTML 文件路径 |
| Swift 外壳 | Go 的启动参数、`NSScreen` 快照、系统时间 | 注入 Theme API v1，维护生命周期状态和两个 DOM class，接收 `done()` |
| HTML 主题 | 固定的 `window.gong`、`html.gong-live`、`html.gong-fired` | 只负责视觉和退场；不能反向读取 Go 配置，也不修改宿主状态 |

### Theme API v1

`window.gong` 是外壳与主题之间唯一的公开 **JavaScript** 运行时接口；`html.gong-live` 和 `html.gong-fired` 是另外两个由宿主维护的生命周期信号。壳用 `WKUserScript` 在主文档的 `.atDocumentStart` 一次性注入完整对象；主题不直接读取 Go 配置，也拿不到定时名、文案、`grace`、`duration` 或 launchd 信息。

下面的类型定义就是 **v1 固定模板**。根字段、`screen` 子字段、名称、类型、单位和回调签名都属于契约：

```ts
type EpochMilliseconds = number; // Unix 毫秒，且是 JavaScript 安全整数

interface GongThemeScreenV1 {
  readonly index: number;
  readonly isMain: boolean;
  readonly primary: boolean;
  readonly w: number;
  readonly h: number;
  readonly scale: number;
}

interface GongThemeAPIV1 {
  readonly apiVersion: 1;
  readonly target: EpochMilliseconds;
  readonly now: EpochMilliseconds;
  readonly lead: number;
  readonly force: boolean;
  readonly revealed: boolean;
  readonly fired: boolean;
  readonly screens: number;
  readonly screen: GongThemeScreenV1;

  onReveal: (() => void) | null;
  onTick: ((now: EpochMilliseconds) => void) | null;
  onFire: (() => void) | null;
  done(): void;
}
```

数据字段：

| 字段 | 类型与单位 | 固定语义 |
|---|---|---|
| `apiVersion` | 整数，恒为 `1` | Theme API 主版本，不跟 gong 的发布版本走 |
| `target` | Unix epoch 毫秒安全整数 | 本次计划目标时刻；同一进程、所有屏幕完全相同且不会改变 |
| `now` | Unix epoch 毫秒安全整数 | 外壳的实际投递时刻；每次公开回调前先刷新，系统校时可能让它回退 |
| `lead` | 整数秒，`0..60` | 计划亮相时刻为 `target - lead * 1000`；迟到时立即亮相，不修改 `target` |
| `force` | boolean | `gong vis` 预览为 `true`；只跳过时间窗和全屏抑制，其他生命周期不变。未显式给目标时刻时，外壳合成 `target = 启动时刻 + lead` |
| `revealed` | boolean | 由外壳维护，只会 `false -> true`；为真时已有 `html.gong-live` |
| `fired` | boolean | 由外壳维护，只会 `false -> true`，并且 `fired => revealed` |
| `screens` | 正整数 | 进程启动时的屏幕数量快照；每屏拥有互不共享的 JS realm 和 DOM |
| `screen` | `GongThemeScreenV1` | 当前 WebView 对应的屏幕快照 |

`screen` 字段：

| 字段 | 类型与单位 | 固定语义 |
|---|---|---|
| `index` | 整数，`0 <= index < screens` | `NSScreen.screens` 的数组位置，没有主屏语义 |
| `primary` | boolean | 同一进程恰好一个屏幕为 `true`；优先取启动时的 `NSScreen.main`，找不到时回退 `index === 0`，主题的单实例逻辑必须使用它 |
| `isMain` | boolean | `NSScreen.main` 的启动快照，仅为 v1 兼容保留；主题不要用它判断 primary |
| `w` / `h` | 正数，逻辑点 | 完整 `screen.frame` 的宽高，在 WebView 中对应逻辑 CSS 尺寸，不是物理像素 |
| `scale` | 正数 | 每逻辑点的 backing pixels；物理尺寸约为 `w * scale`、`h * scale` |

回调和方法：

| 成员 | 谁赋值 | 调用语义 |
|---|---|---|
| `onReveal()` | 主题 | 可选；每个文档至多一次，调用时 `revealed === true` 且 `gong-live` 已存在 |
| `onTick(now)` | 主题 | 可选；亮相后约每 100ms 调用，允许合并、跳帧和长暂停，参数恒等于当次 `gong.now` |
| `onFire()` | 主题 | 可选；每个文档至多一次，调用时 `fired === true` 且 `gong-fired` 已存在 |
| `done()` | 外壳 | 主题退场完成后调用；进程级幂等，只有 `screen.primary` 对应的 WebView 有效 |

三个回调槽在主题代码运行前就存在，初始值固定为 `null`。数据字段由外壳拥有，主题只给回调槽赋函数并调用 `done()`；v1 为兼容旧主题没有冻结整个对象，但主题仍不得改写 `now`、状态字段、屏幕数据或两个 DOM class。`window.gong` 引用和 `apiVersion` 本身不可替换。

#### 固定生命周期

1. **注入**：外壳在 `.atDocumentStart` 建好完整 `window.gong`，主题同步注册需要的回调。页面可以提前加载和预热，但此时 panel 不可见。
2. **Reveal**：到 `target - lead`，或迟到后尽快执行。外壳先显示 panel，再更新 `now`、置 `revealed = true`、添加 `html.gong-live`，最后调用 `onReveal()`。
3. **Tick**：只在 reveal 后投递。约 100ms 是调度间隔，不是保证；主题必须用 `now - startedAt` 之类的绝对差值，不能数心跳次数。
4. **Fire**：到 `target`，或页面晚加载后立即补发。外壳先确保 reveal 已完成，再更新 `now`、置 `fired = true`、添加 `html.gong-fired`，最后调用 `onFire()`。
5. **Done**：主屏主题完成退场后调用 `done()`；外壳只接受主屏 WebView 的第一次消息，然后退出所有 panel。不要依赖 `unload` 做必要清理。
6. **兜底退出**：主题没调用 `done()` 时走可见 timeout；进程另有 150 秒绝对 watchdog，全屏抑制也可能提前终止。

`gong-live` 必须先于 `gong-fired`，两个 class 都只添加一次、永不移除。页面若在 reveal 或 fire 之后、但在进程兜底退出之前加载完成，外壳会按 `reveal -> fire` 顺序回放。纯 CSS 主题可以不注册回调，但只能依靠 timeout 退出。

**`onFire()` 保证早于第一条 `now >= target` 的 `onTick(now)`。** 主题想用哪个进入到点阶段都行——注册 `onFire`，或者在 `onTick` 里自己判断 `now >= target`，两者等价。

这个保证是心跳在派发 tick 之前先补一刀 fire 换来的（`overlay.swift` 的 `every(heartbeatInterval)`）。fire 另有一个专用 timer 负责精度，心跳这一刀只负责顺序，两者取先到的那个。

之前这里写的是反过来的规则——「两个 timer 独立，顺序不承诺，主题必须走 `onFire()`」。那条规则是在拿文档去补实现的洞，而且洞比想象中大：实测专用 fire timer 比心跳**晚 159–483ms**（它跟心跳的 `evaluateJavaScript` 抢主队列，leeway 又是 50ms 对 20ms），所以旧行为下 `TICK >= target` 几乎每次都先于 `onFire()` 到达。补刀之后 fire 的延迟降到 **13–99ms**，顺序也恒定了。

**推论：`fired` 和 `revealed` 这两个状态字段，主题不需要读。** `fired` 等价于 `now >= target`，`revealed` 等价于「收到过 onTick」（心跳是在 reveal 里才启动的）。v1 冻结了字段集所以它们还在对象上，但新主题不该依赖——壳应该只给时间，状态让主题自己从时间推。仓库里两个主题现在都不读它们。

回调不是异步屏障：外壳不会等待返回的 Promise 再投递后续事件。但同步异常、回调返回的 rejected Promise、全局 error 和未处理的 Promise rejection 都会带来源写进外壳 stderr；连续重复的同一来源错误会去重，动态错误也按来源限制为每秒最多一条，避免 `onTick` 刷屏。

#### 版本兼容规则

- `apiVersion` 是整数主版本。v1 的固定字段集不静默增删；根字段或 `screen` 字段新增、删除、改名、改类型、改单位，或者生命周期与回调签名变化，都必须发布新的主版本。
- 旧主题不会读取 `apiVersion`，仍能运行在 v1 外壳上。新主题应先检查自己支持的版本；需要兼容旧外壳时，可以把缺少 `apiVersion` 当作 legacy v0。
- v1 保留 `isMain`，但新的主题逻辑只能使用 `screen.primary`。目录 ID、当前未版本化的 `theme.toml` 元数据和 Theme API 主版本是三件独立的事。

最小主题骨架：

```html
<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<style>
  html, body { margin: 0; height: 100%; background: transparent; }
  .view { opacity: 0; }
  html.gong-live .view { animation: reveal .3s forwards; }
  @keyframes reveal { to { opacity: 1; } }
</style>
</head>
<body>
  <div class="view">...</div>
<script>
(() => {
  const g = window.gong;
  if (g.apiVersion !== 1) throw new Error('unsupported Theme API');

  let firedAt = 0;
  let lastNow = g.now;
  g.onFire = () => { firedAt = g.now; };
  g.onTick = now => {
    lastNow = Math.max(lastNow, now); // 系统校时可能让 epoch 暂时回退
    if (firedAt && lastNow - firedAt >= 1000) g.done();
  };
})();
</script>
</body>
</html>
```

#### 主题实现规则

1. **动画必须挂在 `html.gong-live` 下面。** 页面是被壳提前加载好的，按 `load` 起算会跑偏几十秒。
2. **`target` 是绝对时间戳。** 主题不用关心自己是提前 5 秒还是 60 秒被亮出来的。
3. **主题里不要拿 `setTimeout`、`setInterval` 或 `requestAnimationFrame` 当业务时钟或退出闸门。** `requestAnimationFrame` 可以绘制像素，但进度仍由 `gong.now` 决定。
4. **多屏时每块屏是一个互不知情的 WebView 实例。** `screen.index` 只是数组位置；昂贵或单实例视觉用 `screen.primary` 控制。
5. **常驻 CSS 动画不要挂在带 `filter` 的元素上**，见第四节。

### 时间归壳，像素归主题

页面里的 JS 定时器**不可靠**，这是实测结论，不是洁癖：

| 环境 | 一个 2600ms 的 `setTimeout` 实际什么时候来 |
|---|---|
| 空白页面 | 2601ms（精确到 1ms） |
| nixie（每个字各跑 filter 动画） | 2801ms / 6447ms / **超过 15 秒都不来** |
| nixie（动画收到容器上之后） | 2604ms，但仍见过整轮不来 |

而壳这边的 `DispatchSourceTimer` 通常只偏几毫秒。所以**时间由壳来敲，进度由绝对时间差决定**：

- 壳在 reveal 后约每 100ms 投递一次 `__gongTick(now)`，但允许漏帧、合并和长暂停
- 壳用独立目标 timer 触发 `__gongFire(now)`；迟到或页面晚加载时按 `reveal -> fire` 补发
- 主题用 `now - startedAt` 这样的绝对毫秒差推进，绝不把心跳次数当时间

改完之后旧版 nixie 的回归跑 8 次全部 6.61s（首次冷启 7.17s），default 连跑 4 次全部 5.50s。改之前同样的配置会随机跑出 40s / 48s——那不是「动画卡一点」，是主题永远喊不出 done、浮层挂满 60 秒才被闸门砍。

`onTick` 的投递粒度通常约为 100ms，但业务时刻不因此累计漂移：下一次回调仍携带宿主当下的绝对毫秒值，`onFire` 也不依赖心跳次数。

**这 100ms 是「时刻」的粒度，不必是「画面」的粒度。** 想要满刷新率，就在 rAF 里用 `performance.now` 从最近一次心跳的锚点往前推，见第四节那三条后果——心跳负责校正绝对时间，rAF 负责补中间帧。

## 八、任务清单（建议顺序）

- [x] Swift 壳重构：flag 解析、延迟亮相、`window.gong` 注入、非对称时间窗、全屏复检、双闸门、日志桥、心跳
- [x] 主题跑通契约：`default`（闸门 → 白炽灯丝 → 现在的 LED 信号板，见下面「主题两次推翻」）
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
- 退出走优雅路径时，可以在未来的 Theme API v2 设计 `onDismiss()` 给主题 250ms 收尾，同时挂硬兜底强杀；不能直接把它追加进已冻结的 v1。

## 九、现有代码

代码就在仓库里，**不要再往这份文档里内联 Swift/HTML 源码**——上一版这么干过，结果文档里的 swift 和磁盘上的 swift 悄悄分了叉。

```
overlay.swift          Swift 壳
themes/default/        LED 信号板主题（第三版内容，见下面「主题两次推翻」）
themes/tunnel/         字幕隧道主题（倒数数字迎面冲来，见下面「隧道主题」）
```

编译与自测：

```bash
swiftc -O overlay.swift -o gong-overlay

./gong-overlay --force --theme themes/default/index.html
./gong-overlay --at 12:00:00 --lead 0 --grace 1200 --timeout 20 \
               --tag '#1' --theme themes/default/index.html
```

### 主题两次推翻，`nixie` 已删除

`default` 目录下的内容前后换过三版：最早是闸门（上下两道压边），然后是 `nixie` 白炽灯丝（当时是独立主题，用来验证 Theme API 契约够不够用），现在是移植自用户提供的 React/Canvas 组件的 LED 点阵信号板——原型阶段的两个主题都被判定"太朴素，没人会想用"，整个删掉重做，不是迭代。

`nixie` 目录已经从仓库删除。**它当时验证 Theme API 契约的价值没有消失**——多屏单实例、`screen.primary`、时间归壳这些设计决策都是靠它撞出来的，具体教训还留在本文档的相应小节里（搜"filter 动画把主线程压死"能找到）；删掉的只是那份代码，不是那些教训。

`internal/config/config.go` 的 `Default()` 原来给"午间"那条配了 `nixie`，`nixie` 一删这条默认配置就会在 `gong on` 时报"主题找不到"——已经改成两条默认定时都指 `default`。`internal/agent/agent_test.go` 里两个依赖"某个主题 lead=5"的跨午夜测试，原来抄近路直接写死 `s.Theme = "nixie"`（依赖仓库里恰好有这么一个主题，测试跟具体主题耦合，这次改的时候才发现这个问题），换成了测试自建的临时主题目录，不再依赖仓库里现存哪些主题。

这里的 `--at` 是兼容旧调用方的“按当天解析”参数；生产调度应同时传本次目标的 `--target <Unix 秒>`。`--force` 跳过时间窗和全屏判断，是 `gong vis` 的雏形。主题里的 `console.log` 和未捕获异常会打到 stderr，前缀 `gong-overlay[js]`。

### 隧道主题（`tunnel`）：从 React 组件移植过来的三个决定

原件是一个 framer-motion 组件（ZoomTextTunnel / "Infinite Text Passage"）：两个 slot 轮流用，词从 `scale(.05)` 进场到 1，上一个词放大到 35 倍并淡出，`while(true)` 配 `setTimeout(hold)` 无限循环。移植到 gong 时有三处**不能照抄**：

1. **无限循环必须改成由 `gong.now` 驱动。** 原件的节奏是 `hold(600ms) + duration(1.2s)`，靠 `await settle()` 保证两段动画永不重叠。gong 这边节奏由绝对时间定死——倒数显示的数字是 `ceil((target - now)/1000)` **现算**的，不是数心跳数出来的。好处是心跳抖动、页面卡顿甚至整段掉帧，数字都还是对的；坏处是 1 秒的间隔比 1.2 秒的过渡短，两个 slot 必然撞车。

2. **两个 slot 改成一个词一个节点。** 撞车的直接后果是：新词要复用的那个 slot 还在半路上飞（opacity 还有一半），把它瞬间掰回 `scale(.06)` 会看见一次明显的跳变。改成新词进场时只通知上一个词开始飞出去、不等它，被打断的词带着当前 scale 继续飞（CSS 过渡本来就能从中途接管）。实测同屏最多 3 个词在不同深度，反而比原件更像隧道。节点回收不挂 `transitionend`，统一在 `onTick` 里按绝对时间扫——**全场只有一个时钟**。

3. **`transition` 里的 `stiffness / damping / mass` 是死参数。** 原件写了这三个，但 `type` 是 `"tween"`，framer-motion 在 tween 模式下根本不读它们；真正生效的只有 `duration` 和 `ease`。所以 CSS 侧只需要一条 `1.2s cubic-bezier(.7,0,.25,1)` 就是一比一等价，不用去凑弹簧参数。

另外两点：倒数几个数由壳注入的 `gong.lead` 决定，不在主题里再写死一份——否则 `theme.toml` 的 lead 改成 10 而主题还从 5 开始数，前 5 秒就是一片没有字的压暗，看着像主题坏了。字体没用原 props 指定的 Inter：主题不能发外部请求也没随包带字体文件，走 `-apple-system`（WKWebView 里就是 SF Pro），大字号下观感接近。

---

## 十、发版踩的坑

- **`.gitignore` 的模式不带前导 `/` 会匹配任意层级。** 写了 `gong` 想忽略根目录的编译产物，结果把 `cmd/gong/` 整个目录也忽略了，初版提交里没有 Go 的 main 包，CI 在 `go build ./cmd/gong` 直接挂。产物模式一律写成 `/gong` 这种锚定形式。
  更值得记的是**怎么发现的**：`git add -A` 之后我打印了 staged 列表却没逐项核对。可靠的做法是拿 `git ls-files` 跟磁盘 `find` 做 `comm -23` 差集，空集才算齐。
- **CI 的 Go 版本别写死**，用 `go-version-file: go.mod`，否则 `go.mod` 一升版就挂。
- **release 的 sha256 要自己下载复核一遍**，不要直接抄 CI 输出的那行——formula 里填错 sha 的表现是所有人都装不上。
- **发版前先看看仓库里已经有什么。** 我照 doc 里那句「tap `homebrew-gong`」直接建了个新 tap，其实 `xwvike/homebrew-tap` 早就存在（goreleaser 在维护 local-mirror 的 cask）。而 doc 当时是**自相矛盾**的——第六节写 tap 叫 `homebrew-gong`，安装命令却写着 `xwvike/tap/gong`；我把矛盾按「改命令迁就仓库名」解决了，正好改反。
  文档内部打架时，**去查现实**（`gh repo list`）再决定听谁的，不要挑一个顺手的。
  顺带：`xwvike/tap` 这种通用 tap 名也比一个软件一个 tap 好——多个软件共用一个 tap 是 Homebrew 的常规做法。
- **假 HOME 隔离不了 launchd。** 测试时用 `HOME=/tmp/xxx` 跑 `gong on/off/uninstall`，文件路径确实被隔离了，但 `launchctl bootout gui/$UID/local.gong` 用的是**全局 label**，跟 HOME 无关——所以假 HOME 里的一次 uninstall 会把真实系统上那个同名 job 一起卸掉，而 plist 文件还留在真 HOME 里，表现是「文件在、`gong ls` 说未接管」。
  写这段时就这么把自己的 job 卸了一次。以后跑这类测试，**结束后一定要 `gong on` 复位并用 `launchctl list | grep gong` 确认**，或者给测试用的 label 加前缀。
  **这条踩过第二次**——测 `gong rm` 的确认流程时，同样的假 HOME 手法又把真实 `local.gong` job 卸掉了一次。恢复时又多摔了一跤：随手拿本地开发编译的 `gong` 跑了一次 `gong on` 复位，结果 plist 里的 `ProgramArguments` 被重写成项目目录里那个开发中的二进制路径，而不是 `/opt/homebrew/bin/gong`——复位之后**必须用 `which gong` 确认走的是稳定路径**，而不是手边随便一个能跑的 `gong`。教训已经写进跨会话记忆里了，靠事后检查兜底，别指望自己不会再忘。
