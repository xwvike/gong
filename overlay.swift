// gong-overlay — 到点在所有屏幕最顶层播一段 HTML 动画，不抢焦点、不吃点击，播完自杀。
// 编译： swiftc -O overlay.swift -o gong-overlay
// 运行： ./gong-overlay --at 12:00:00 --lead 5 --theme ~/.config/gong/themes/nixie/index.html
//        ./gong-overlay --force --theme ./themes/nixie/index.html      # 预览
//
// 这个壳是无状态的：不读配置文件、不认识主题名、不知道有几条定时。
// 传进来的 flag 全是【什么时候】和【活多久】，没有一个是【长什么样】——
// 提醒的形式和文案由主题自己决定，壳不参与。

import Cocoa
import WebKit

// ── 写死的闸门。主题改不了，flag 也调不高。 ────────────────
let maxVisibleSeconds = 60.0    // 浮层可见时长上限，从 orderFront 那一刻算
let maxProcessSeconds = 150.0   // 进程总时长绝对兜底，防等待定时器本身出事
let maxLeadSeconds = 60         // 提前亮相上限
let earlySlackSeconds = 90.0    // 时间窗左边界的余量：launchd 只有分钟精度
let recheckInterval = 5.0       // 亮相后多久复检一次全屏
let heartbeatInterval = 0.1     // 壳给页面喂时间的节拍
// ──────────────────────────────────────────────────────

struct Options {
    var at: (h: Int, m: Int, s: Int)?
    var lead = 0
    var grace = 1200            // 秒。超出这个窗口就不放（防止睡醒后 launchd 补播）
    var timeout = 20.0          // 主题不喊 done 时的可见兜底
    var name = ""               // 只用来给 stderr 打标，不注入页面
    var theme = ""
    var force = false           // 跳过时间窗和全屏判断，gong vis 预览走这条
}

func parseArgs() -> Options {
    var o = Options()
    var pending: [String] = []
    for raw in CommandLine.arguments.dropFirst() {
        if raw.hasPrefix("--"), let eq = raw.firstIndex(of: "=") {
            pending.append(String(raw[raw.startIndex..<eq]))
            pending.append(String(raw[raw.index(after: eq)...]))
        } else {
            pending.append(raw)
        }
    }

    var i = 0
    func next() -> String? {
        guard i + 1 < pending.count else { return nil }
        i += 1
        return pending[i]
    }
    while i < pending.count {
        let arg = pending[i]
        switch arg {
        case "--force":   o.force = true
        case "--at":      o.at = parseClock(next() ?? "")
        case "--lead":    o.lead = min(max(Int(next() ?? "") ?? 0, 0), maxLeadSeconds)
        case "--grace":   o.grace = max(Int(next() ?? "") ?? 1200, 0)
        case "--timeout": o.timeout = min(Double(next() ?? "") ?? 20, maxVisibleSeconds)
        case "--name":    o.name = next() ?? ""
        case "--theme":   o.theme = next() ?? ""
        case let other where other.hasPrefix("--"):
            // 认不出来的 flag 要喊一声：Go 侧改了参数而壳没跟上时，
            // 沉默的后果是它的值被当成主题路径吞掉，然后一切静悄悄地跑歪。
            FileHandle.standardError.write("gong-overlay: unknown flag \(other)\n".data(using: .utf8)!)
        default:
            // 位置参数当主题路径，兼容老的 `gong-overlay <html>` 调法
            if o.theme.isEmpty { o.theme = arg }
        }
        i += 1
    }
    return o
}

/// "18:00" / "18:00:00" → (18, 0, 0)
func parseClock(_ s: String) -> (h: Int, m: Int, s: Int)? {
    let parts = s.split(separator: ":").map { Int($0) }
    guard parts.count >= 2, let h = parts[0], let m = parts[1], (0...23).contains(h), (0...59).contains(m)
    else { return nil }
    let sec = parts.count > 2 ? (parts[2] ?? 0) : 0
    return (h, m, sec)
}

/// 关键：nonactivatingPanel + 永不成为 key window，这是「盖在上面但不夺焦点」的唯一正解。
/// 用普通 NSWindow 做不到，用 mpv / Chrome --app 也做不到。
final class OverlayPanel: NSPanel {
    override var canBecomeKey: Bool { false }
    override var canBecomeMain: Bool { false }
}

final class AppDelegate: NSObject, NSApplicationDelegate, WKScriptMessageHandler, WKNavigationDelegate {
    private var panels: [OverlayPanel] = []
    private var webs: [WKWebView] = []
    private var loaded: Set<ObjectIdentifier> = []
    private var timers: [DispatchSourceTimer] = []
    private var opts = Options()
    private var revealed = false

    func applicationDidFinishLaunching(_ note: Notification) {
        // .accessory = 无 Dock 图标、不接管菜单栏、启动时不激活
        NSApp.setActivationPolicy(.accessory)

        opts = parseArgs()
        let now = Date()
        let target = resolveTarget(now: now)

        guard opts.force || (insideTimeWindow(now: now, target: target) && !fullscreenAppInFront())
        else { NSApp.terminate(nil); return }

        let path = opts.theme.isEmpty
            ? (NSHomeDirectory() as NSString).appendingPathComponent(".config/gong/themes/default/index.html")
            : (opts.theme as NSString).expandingTildeInPath
        let html = URL(fileURLWithPath: path)
        guard FileManager.default.fileExists(atPath: html.path) else {
            FileHandle.standardError.write("gong-overlay: theme not found: \(html.path)\n".data(using: .utf8)!)
            NSApp.terminate(nil); return
        }

        // 先把 panel 建好、页面加载好，但【不 orderFront】——屏幕上什么都没有。
        // 等到 target - lead 才亮相，这样首帧是零延迟的，不会先闪一下白底。
        let screens = NSScreen.screens
        for (idx, screen) in screens.enumerated() {
            let (panel, web) = makePanel(on: screen, index: idx, total: screens.count,
                                         html: html, target: target, now: now)
            panels.append(panel)
            webs.append(web)
        }

        let revealAt = target.addingTimeInterval(-Double(opts.lead))
        after(max(revealAt.timeIntervalSince(now), 0)) { [weak self] in self?.reveal() }

        // 到点这一下也由壳来敲，主题不用自己盯着 target
        after(max(target.timeIntervalSince(now), 0)) { [weak self] in
            guard let self else { return }
            self.eachLoadedWeb { $0.evaluateJavaScript(
                "window.__gongFire && window.__gongFire(\(self.nowMillis()))", completionHandler: nil) }
        }

        // 绝对兜底：无论上面哪一步卡住，进程都不会活过这个时间
        after(maxProcessSeconds) { NSApp.terminate(nil) }
    }

    private func eachLoadedWeb(_ body: (WKWebView) -> Void) {
        for w in webs where loaded.contains(ObjectIdentifier(w)) { body(w) }
    }

    /// 用 DispatchSourceTimer 而不是 asyncAfter：后者的 leeway 跟间隔成正比，
    /// 实测 60 秒的闸门会飘到 63 秒。闸门是给用户兜底的，不能由系统看着办。
    private func after(_ seconds: Double, _ body: @escaping () -> Void) {
        let t = DispatchSource.makeTimerSource(queue: .main)
        t.schedule(deadline: .now() + seconds, leeway: .milliseconds(50))
        t.setEventHandler(handler: body)
        t.resume()
        timers.append(t)          // 必须持有，否则 source 立刻被释放，回调永远不来
    }

    private func every(_ seconds: Double, _ body: @escaping () -> Void) {
        let t = DispatchSource.makeTimerSource(queue: .main)
        t.schedule(deadline: .now() + seconds, repeating: seconds, leeway: .milliseconds(20))
        t.setEventHandler(handler: body)
        t.resume()
        timers.append(t)
    }

    /// --force 时没有真实的目标时刻，就拿「现在 + lead」当靶子，
    /// 这样倒计时类主题预览时也有东西可倒。
    private func resolveTarget(now: Date) -> Date {
        guard let at = opts.at else { return now.addingTimeInterval(Double(opts.lead)) }
        var c = Calendar.current.dateComponents([.year, .month, .day], from: now)
        (c.hour, c.minute, c.second) = (at.h, at.m, at.s)
        return Calendar.current.date(from: c) ?? now.addingTimeInterval(Double(opts.lead))
    }

    private func reveal() {
        guard !revealed else { return }
        revealed = true
        for p in panels { p.orderFrontRegardless() }   // 注意不是 makeKeyAndOrderFront
        for w in webs where loaded.contains(ObjectIdentifier(w)) { fireReveal(w) }
        // 可见时长闸门，从这一刻开始算
        after(min(opts.timeout, maxVisibleSeconds)) { NSApp.terminate(nil) }

        // 心跳：由壳来喂时间，主题不要自己起 setInterval/setTimeout 来卡点。
        // 实测页面里的 JS 定时器在重绘压力下会被拖到几秒甚至不来，
        // 而壳这边的 DispatchSourceTimer 误差只有几毫秒。时间归壳，像素归主题。
        every(heartbeatInterval) { [weak self] in
            guard let self else { return }
            self.eachLoadedWeb { $0.evaluateJavaScript(
                "window.__gongTick && window.__gongTick(\(self.nowMillis()))",
                completionHandler: nil) }
        }
        guard !opts.force else { return }
        // 亮相后用户可能中途进全屏（开个全屏会议），浮层在 screenSaver 层会盖在上面
        Timer.scheduledTimer(withTimeInterval: recheckInterval, repeats: true) { _ in
            if self.fullscreenAppInFront() { NSApp.terminate(nil) }
        }
    }

    private func makePanel(on screen: NSScreen, index: Int, total: Int,
                           html: URL, target: Date, now: Date) -> (OverlayPanel, WKWebView) {
        let panel = OverlayPanel(contentRect: screen.frame,
                                 styleMask: [.borderless, .nonactivatingPanel],
                                 backing: .buffered,
                                 defer: false)

        // screenSaver 层 (1000) 才盖得住别的 App 的全屏窗口；普通 .floating (3) 会被压在下面
        panel.level = NSWindow.Level(rawValue: Int(CGWindowLevelForKey(.screenSaverWindow)))
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = false
        panel.ignoresMouseEvents = true          // 点击穿透，鼠标当它不存在
        panel.isReleasedWhenClosed = false
        panel.collectionBehavior = [.canJoinAllSpaces, .stationary,
                                    .ignoresCycle, .fullScreenAuxiliary]

        let cfg = WKWebViewConfiguration()
        cfg.userContentController.add(self, name: "done")   // JS 里调 done 就退出
        cfg.userContentController.add(self, name: "log")    // console.log / 未捕获异常 → stderr
        cfg.userContentController.addUserScript(
            WKUserScript(source: bootstrapJS(screen: screen, index: index, total: total,
                                             target: target, now: now),
                         injectionTime: .atDocumentStart,
                         forMainFrameOnly: true))

        let web = WKWebView(frame: screen.frame, configuration: cfg)
        web.navigationDelegate = self

        // 让 WebView 背景真透明。前者是公开 API（macOS 12+），后者是历来管用的那个 KVC 写法，
        // 两个都写是因为不同系统版本上表现不完全一致。
        web.underPageBackgroundColor = .clear
        web.setValue(false, forKey: "drawsBackground")
        if #available(macOS 13.3, *) { web.isInspectable = true }   // 主题作者用 Safari 调试

        web.loadFileURL(html, allowingReadAccessTo: html.deletingLastPathComponent())
        panel.contentView = web
        return (panel, web)
    }

    /// 注入契约。壳只给【时间】和【几何】，提醒长什么样、说什么全归主题。
    /// 刻意不注入 name/message 之类的内容参数——那会诱导主题去按定时名分支，
    /// 等于把表现逻辑漏回配置层。要不同的提醒形式就写不同的主题。
    private func bootstrapJS(screen: NSScreen, index: Int, total: Int,
                             target: Date, now: Date) -> String {
        let gong: [String: Any] = [
            "target": (target.timeIntervalSince1970 * 1000).rounded(),
            "now": (now.timeIntervalSince1970 * 1000).rounded(),
            "lead": opts.lead,
            "force": opts.force,
            "revealed": false,
            "fired": false,
            "screens": total,
            "screen": [
                "index": index,
                "isMain": screen == NSScreen.main,
                "w": screen.frame.width,
                "h": screen.frame.height,
                "scale": screen.backingScaleFactor,
            ],
        ]
        let json = (try? JSONSerialization.data(withJSONObject: gong))
            .flatMap { String(data: $0, encoding: .utf8) } ?? "{}"

        return """
        (function () {
          window.gong = \(json);
          // 壳在 target - lead 那一刻调这个。CSS 动画要挂在 html.gong-live 下面，
          // 因为页面是提前加载好的，按 load 起算会跑偏。
          // 注意所有回调都先刷新 gong.now 再调主题。
          // gong.now 是注入时写死的启动时刻，而页面是提前加载好的——
          // 亮相可能发生在启动后 60 秒，那时候还拿 gong.now 当「现在」会差整整一个等待期。
          window.__gongReveal = function (ms) {
            if (window.gong.revealed) return;
            window.gong.now = ms || Date.now();
            window.gong.revealed = true;
            document.documentElement.classList.add('gong-live');
            try { window.gong.onReveal && window.gong.onReveal(); } catch (e) {}
          };

          // 壳每 100ms 喂一次当前时间；主题要计时就用它，别自己起定时器。
          window.__gongTick = function (ms) {
            window.gong.now = ms;
            try { window.gong.onTick && window.gong.onTick(ms); } catch (e) {}
          };

          // 到点。壳按 target 精确敲这一下，主题不用自己盯着倒数。
          window.__gongFire = function (ms) {
            if (window.gong.fired) return;
            window.gong.now = ms || Date.now();
            window.gong.fired = true;
            document.documentElement.classList.add('gong-fired');
            try { window.gong.onFire && window.gong.onFire(); } catch (e) {}
          };
          // 主题契约：动画结束必须喊这一声，壳才会退出。没喊到的话有兜底超时。
          // 多屏时每块屏是一个互不知情的 WebView 实例，任何一个喊 done 都会带走整个进程，
          // 所以只认 0 号屏那一份，其余的调了也没用。
          window.gong.done = function () {
            if (window.gong.screen.index !== 0) return;
            window.webkit.messageHandlers.done.postMessage(1);
          };

          // 主题跑在一个没有开发者工具的 WebView 里，出错会静悄悄地什么都不发生。
          // 把 console 和未捕获异常引到壳的 stderr，写主题时用 --force 就能看见。
          var send = function (kind, text) {
            try { window.webkit.messageHandlers.log.postMessage(kind + ' ' + text); } catch (e) {}
          };
          ['log', 'warn', 'error'].forEach(function (k) {
            var orig = console[k];
            console[k] = function () {
              send(k, Array.prototype.map.call(arguments, String).join(' '));
              orig.apply(console, arguments);
            };
          });
          window.addEventListener('error', function (e) {
            send('error', (e.message || 'error') + ' @' + (e.lineno || '?'));
          });
        })();
        """
    }

    private func nowMillis() -> Int { Int(Date().timeIntervalSince1970 * 1000) }

    private func fireReveal(_ web: WKWebView) {
        web.evaluateJavaScript("window.__gongReveal && window.__gongReveal(\(nowMillis()))",
                               completionHandler: nil)
    }

    /// reveal 和页面加载完成谁先谁后是不定的：lead=0 时 reveal 几乎立刻发生，
    /// 那会儿 __gongReveal 还不存在，喊了也白喊。所以两边都要触发一次。
    func webView(_ web: WKWebView, didFinish navigation: WKNavigation!) {
        loaded.insert(ObjectIdentifier(web))
        if revealed { fireReveal(web) }
    }

    func userContentController(_ c: WKUserContentController, didReceive m: WKScriptMessage) {
        guard m.name == "done" else {
            let tag = opts.name.isEmpty ? "" : "[\(opts.name)]"
            FileHandle.standardError.write("gong-overlay\(tag)[js] \(m.body)\n".data(using: .utf8)!)
            return
        }
        NSApp.terminate(nil)
    }

    // MARK: - 抑制条件

    /// launchd 的 StartCalendarInterval 在错过的时间点会在唤醒后补跑一次。
    /// 没这个判断的话，晚上十点开电脑会被祝贺下班。
    ///
    /// 窗口是【非对称】的：launchd 只有分钟精度，lead=5 的定时实际是提前 60 秒被拉起来的，
    /// 所以左边界要放到 target - lead - 90s，右边界还是 grace。
    private func insideTimeWindow(now: Date, target: Date) -> Bool {
        let delta = now.timeIntervalSince(target)
        return delta >= -(Double(opts.lead) + earlySlackSeconds) && delta <= Double(opts.grace)
    }

    /// 启发式：全屏 App 会藏起菜单栏和 Dock，此时 visibleFrame 等于 frame。
    /// 缺点是开了「自动隐藏菜单栏」会误判。要更准就换成 CGWindowListCopyWindowInfo
    /// 遍历 layer 0 的窗口、看有没有 bounds 铺满整屏的——只读 bounds，绝对不要读窗口标题，
    /// 读标题会触发屏幕录制权限申请。
    private func fullscreenAppInFront() -> Bool {
        guard let s = NSScreen.main else { return false }
        return s.visibleFrame.height >= s.frame.height
    }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.run()
