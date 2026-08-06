// gong-overlay 在所有屏幕顶层播放无焦点、点击穿透的 HTML 提示。
// 编译： swiftc -O overlay.swift -o gong-overlay
// 预览： ./gong-overlay --force --theme ./themes/default/index.html

import Cocoa
import CoreGraphics
import WebKit
import Darwin

// ── 写死的闸门。主题改不了，flag 也调不高。 ────────────────
let maxVisibleSeconds = 60.0    // 浮层可见时长上限，从 orderFront 那一刻算
let maxProcessSeconds = 150.0   // 进程总时长绝对兜底，防等待定时器本身出事
let maxLeadSeconds = 60         // 提前亮相上限
let earlySlackSeconds = 90.0    // 时间窗左边界的余量：launchd 只有分钟精度
let recheckInterval = 5.0       // 亮相后多久复检一次全屏
let heartbeatInterval = 0.1     // 壳给页面喂时间的节拍
let themeAPIVersion = 1         // 只表示 window.gong 契约主版本，不跟 gong 发布版本走
let maxJavaScriptSafeInteger = 9_007_199_254_740_991.0
// ──────────────────────────────────────────────────────

private struct ThemeAPIScreenV1: Encodable {
    let index: Int
    let isMain: Bool
    let primary: Bool
    let w: Double
    let h: Double
    let scale: Double
}

private struct ThemeAPIContextV1: Encodable {
    let apiVersion: Int
    let target: Int64
    let now: Int64
    let lead: Int
    let force: Bool
    let revealed: Bool
    let fired: Bool
    let screens: Int
    let screen: ThemeAPIScreenV1
}

struct Options {
    var at: (h: Int, m: Int, s: Int)?
    // Go 侧传入本次触发对应的绝对 Unix 时间，避免跨午夜时按「今天」解析 --at。
    var targetEpoch: Double?
    var lead = 0
    var grace = 1200            // 秒。超出这个窗口就不放（防止睡醒后 launchd 补播）
    var timeout = 20.0          // 主题不喊 done 时的可见兜底
    // 只用来给 stderr 打标，不注入页面。刻意不叫 name：定时没有名字这回事，
    // Go 侧传进来的是「#序号」或 "vis"，纯粹为了在日志里认出是哪次触发。
    var tag = ""
    var theme = ""
    var force = false           // 跳过时间窗和全屏判断，gong vis 预览走这条
    var invalid = false
}

/// Theme API 用 JavaScript number 表示 epoch 毫秒，必须留在安全整数范围内。
func epochMilliseconds(_ seconds: Double) -> Int64? {
    let milliseconds = seconds * 1000
    guard milliseconds.isFinite, abs(milliseconds) <= maxJavaScriptSafeInteger else { return nil }
    return Int64(milliseconds.rounded(.towardZero))
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
    func reportInvalid(_ message: String) {
        o.invalid = true
        FileHandle.standardError.write("gong-overlay: \(message)\n".data(using: .utf8)!)
    }
    while i < pending.count {
        let arg = pending[i]
        switch arg {
        case "--force":   o.force = true
        case "--at":
            let raw = next() ?? ""
            if let at = parseClock(raw) {
                o.at = at
            } else {
                reportInvalid("invalid --at")
            }
        case "--target":
            if let raw = next(), let epoch = Double(raw), epochMilliseconds(epoch) != nil {
                o.targetEpoch = epoch
            } else {
                reportInvalid("invalid --target")
            }
        case "--lead":
            if let raw = next(), let value = Int(raw) {
                o.lead = min(max(value, 0), maxLeadSeconds)
            } else {
                reportInvalid("invalid --lead")
            }
        case "--grace":
            if let raw = next(), let value = Int(raw) {
                o.grace = max(value, 0)
            } else {
                reportInvalid("invalid --grace")
            }
        case "--timeout":
            if let raw = next(), let value = Double(raw), value.isFinite {
                o.timeout = min(max(value, 0), maxVisibleSeconds)
            } else {
                reportInvalid("invalid --timeout")
            }
        case "--tag":
            if let value = next() {
                o.tag = value
            } else {
                reportInvalid("missing --tag value")
            }
        case "--theme":
            if let value = next() {
                o.theme = value
            } else {
                reportInvalid("missing --theme value")
            }
        case let other where other.hasPrefix("--"):
            // 认不出来的 flag 要喊一声：Go 侧改了参数而壳没跟上时，
            // 沉默的后果是它的值被当成主题路径吞掉，然后一切静悄悄地跑歪。
            reportInvalid("unknown flag \(other)")
        default:
            // 位置参数当主题路径，兼容老的 `gong-overlay <html>` 调法
            if o.theme.isEmpty { o.theme = arg }
        }
        i += 1
    }
    // 主题路径由 Go 侧解析；壳不读取配置或猜测安装目录。
    if o.theme.isEmpty {
        reportInvalid("missing --theme")
    }
    return o
}

/// "18:00" / "18:00:00" → (18, 0, 0)
func parseClock(_ s: String) -> (h: Int, m: Int, s: Int)? {
    let parts = s.split(separator: ":", omittingEmptySubsequences: false)
    guard (2...3).contains(parts.count),
          let h = Int(parts[0]), let m = Int(parts[1]),
          (0...23).contains(h), (0...59).contains(m)
    else { return nil }
    let sec = parts.count == 3 ? (Int(parts[2]) ?? -1) : 0
    guard (0...59).contains(sec) else { return nil }
    return (h, m, sec)
}

/// nonactivatingPanel 且永不成为 key window，保证浮层不夺焦点。
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
    private var fired = false
    /// 心跳用目标时刻保证 fire 先于到点后的首个 tick。
    private var target = Date.distantFuture
    private var finished = false
    private weak var primaryWeb: WKWebView?

    func applicationDidFinishLaunching(_ note: Notification) {
        // .accessory = 无 Dock 图标、不接管菜单栏、启动时不激活
        NSApp.setActivationPolicy(.accessory)

        opts = parseArgs()
        guard !opts.invalid else { Darwin.exit(2) }
        let now = Date()
        let target = resolveTarget(now: now)

        guard opts.force || (insideTimeWindow(now: now, target: target) && !fullscreenAppInFront())
        else { Darwin.exit(0) }

        // parseArgs 已经保证 theme 非空，这里只做 ~ 展开
        let html = URL(fileURLWithPath: (opts.theme as NSString).expandingTildeInPath)
        guard FileManager.default.fileExists(atPath: html.path) else {
            FileHandle.standardError.write("gong-overlay: theme not found: \(html.path)\n".data(using: .utf8)!)
            Darwin.exit(1)
        }

        // 提前加载但不展示，直到 target - lead 才亮相。
        let screens = NSScreen.screens
        guard !screens.isEmpty else { Darwin.exit(1) }
        let mainScreen = NSScreen.main
        let primaryIndex = mainScreen.flatMap { main in
            screens.firstIndex(where: { $0 == main })
        } ?? 0
        for (idx, screen) in screens.enumerated() {
            let (panel, web) = makePanel(on: screen, index: idx, total: screens.count,
                                         isMain: mainScreen.map { $0 == screen } ?? false,
                                         primary: idx == primaryIndex,
                                         html: html, target: target, now: now)
            panels.append(panel)
            webs.append(web)
            if idx == primaryIndex { primaryWeb = web }
        }

        self.target = target

        let revealAt = target.addingTimeInterval(-Double(opts.lead))
        after(max(revealAt.timeIntervalSince(now), 0)) { [weak self] in self?.reveal() }

        // 到点这一下也由壳来敲，主题不用自己盯着 target
        after(max(target.timeIntervalSince(now), 0)) { [weak self] in self?.fire() }

        // 绝对兜底：无论上面哪一步卡住，进程都不会活过这个时间
        after(maxProcessSeconds) { NSApp.terminate(nil) }
    }

    private func eachLoadedWeb(_ body: (WKWebView) -> Void) {
        for w in webs where loaded.contains(ObjectIdentifier(w)) { body(w) }
    }

    /// DispatchSourceTimer 为退出闸门提供明确的容差。
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

    /// 预览模式用 now + lead 构造目标时刻。
    private func resolveTarget(now: Date) -> Date {
        // 优先使用 Go 还原的绝对时刻；--at 仅用于兼容和手工调试。
        if let epoch = opts.targetEpoch {
            return Date(timeIntervalSince1970: epoch)
        }
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

        // 宿主心跳提供权威时间，主题负责帧间视觉插值。
        every(heartbeatInterval) { [weak self] in
            guard let self else { return }
            // 保证 target 之后的首个 tick 前已按 reveal -> fire 顺序派发。
            if !self.fired, Date() >= self.target { self.fire() }
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

    /// fire 必须建立在 reveal 之后。页面若还没加载完，只记录全局状态，
    /// didFinish 会为那一屏按 reveal → fire 的顺序补发。
    private func fire() {
        if !revealed { reveal() }
        guard !fired else { return }
        fired = true
        for web in webs where loaded.contains(ObjectIdentifier(web)) { fireFire(web) }
    }

    private func makePanel(on screen: NSScreen, index: Int, total: Int,
                           isMain: Bool, primary: Bool,
                           html: URL, target: Date, now: Date) -> (OverlayPanel, WKWebView) {
        let panel = OverlayPanel(contentRect: screen.frame,
                                 styleMask: [.borderless, .nonactivatingPanel],
                                 backing: .buffered,
                                 defer: false)

        // screenSaver 层可以覆盖其他 App 的全屏窗口。
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
                                             isMain: isMain, primary: primary,
                                             target: target, now: now),
                         injectionTime: .atDocumentStart,
                         forMainFrameOnly: true))

        let web = WKWebView(frame: screen.frame, configuration: cfg)
        web.navigationDelegate = self

        // 公开 API 与 KVC 兼顾不同系统版本的透明背景行为。
        web.underPageBackgroundColor = .clear
        web.setValue(false, forKey: "drawsBackground")
        if #available(macOS 13.3, *) { web.isInspectable = true }   // 主题作者用 Safari 调试

        web.loadFileURL(html, allowingReadAccessTo: html.deletingLastPathComponent())
        panel.contentView = web
        return (panel, web)
    }

    /// 注入只包含时间和屏幕几何；表现内容完全由主题定义。
    private func bootstrapJS(screen: NSScreen, index: Int, total: Int,
                             isMain: Bool, primary: Bool,
                             target: Date, now: Date) -> String {
        guard let targetMillis = epochMilliseconds(target.timeIntervalSince1970),
              let initialNowMillis = epochMilliseconds(now.timeIntervalSince1970)
        else {
            FileHandle.standardError.write(
                "gong-overlay: Theme API time is outside the JavaScript safe integer range\n"
                    .data(using: .utf8)!)
            Darwin.exit(1)
        }
        let context = ThemeAPIContextV1(
            apiVersion: themeAPIVersion,
            target: targetMillis,
            now: initialNowMillis,
            lead: opts.lead,
            force: opts.force,
            revealed: false,
            fired: false,
            screens: total,
            screen: ThemeAPIScreenV1(
                index: index,
                isMain: isMain,
                primary: primary,
                w: Double(screen.frame.width),
                h: Double(screen.frame.height),
                scale: Double(screen.backingScaleFactor)
            )
        )
        let data: Data
        do {
            data = try JSONEncoder().encode(context)
        } catch {
            FileHandle.standardError.write(
                "gong-overlay: encode Theme API v1: \(error)\n".data(using: .utf8)!)
            Darwin.exit(1)
        }
        guard let json = String(data: data, encoding: .utf8) else {
            FileHandle.standardError.write(
                "gong-overlay: encode Theme API v1: invalid UTF-8\n".data(using: .utf8)!)
            Darwin.exit(1)
        }

        return """
        (function () {
          var gong = \(json);
          // v1 的回调槽始终存在；null 表示主题不关心这个事件。
          gong.onReveal = null;
          gong.onTick = null;
          gong.onFire = null;

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

          var reports = Object.create(null);
          var describe = function (error) {
            return error && (error.stack || error.message) || String(error);
          };
          var report = function (scope, error) {
            var text = scope + ': ' + describe(error);
            var now = Date.now();
            var previous = reports[scope];
            if (previous && (previous.text === text || now - previous.at < 1000)) return;
            reports[scope] = {text: text, at: now};
            send('error', text);
          };
          var invoke = function (name, args) {
            var callback = gong[name];
            if (callback == null) return;
            if (typeof callback !== 'function') {
              report('gong.' + name, new TypeError('expected function or null'));
              return;
            }
            try {
              var result = callback.apply(gong, args || []);
              if (result && typeof result.then === 'function') {
                Promise.resolve(result).catch(function (error) {
                  report('gong.' + name, error);
                });
              }
            } catch (error) {
              report('gong.' + name, error);
            }
          };
          var normalizeMillis = function (ms) {
            if (typeof ms !== 'number' || !Number.isFinite(ms)) return Date.now();
            var value = Math.trunc(ms);
            return Number.isSafeInteger(value) ? value : Date.now();
          };

          var doneSent = false;
          gong.done = function () {
            if (doneSent || !gong.screen.primary) return;
            try {
              window.webkit.messageHandlers.done.postMessage(1);
              doneSent = true;
            } catch (error) {
              report('gong.done', error);
            }
          };
          Object.defineProperty(gong, 'apiVersion', {
            value: gong.apiVersion, writable: false, enumerable: true, configurable: false
          });
          Object.defineProperty(window, 'gong', {
            value: gong, writable: false, enumerable: true, configurable: false
          });

          // 私有状态才是宿主幂等依据；公开字段只是给主题读取的状态镜像。
          var didReveal = false;
          var didFire = false;

          // 壳在 target - lead 附近亮相；晚加载时会在 didFinish 立即补发。
          window.__gongReveal = function (ms) {
            if (didReveal) return;
            didReveal = true;
            gong.now = normalizeMillis(ms);
            gong.revealed = true;
            document.documentElement.classList.add('gong-live');
            invoke('onReveal');
          };

          // 心跳约每 100ms 到一次，允许跳帧；主题必须用 now 的绝对差值计时。
          window.__gongTick = function (ms) {
            if (!didReveal) return;
            gong.now = normalizeMillis(ms);
            invoke('onTick', [gong.now]);
          };

          // fire 永远建立在 reveal 之后；晚加载时同样按这个顺序回放。
          window.__gongFire = function (ms) {
            if (!didReveal) window.__gongReveal(ms);
            if (didFire) return;
            didFire = true;
            gong.now = normalizeMillis(ms);
            gong.fired = true;
            document.documentElement.classList.add('gong-fired');
            invoke('onFire');
          };

          window.addEventListener('error', function (e) {
            var location = (e.filename || '?') + ':' + (e.lineno || '?') + ':' + (e.colno || '?');
            report('window.error', e.error || ((e.message || 'error') + ' @' + location));
          });
          window.addEventListener('unhandledrejection', function (e) {
            report('unhandledrejection', e.reason);
          });
        })();
        """
    }

    private func nowMillis() -> Int64 {
        guard let value = epochMilliseconds(Date().timeIntervalSince1970) else { Darwin.exit(1) }
        return value
    }

    private func fireReveal(_ web: WKWebView) {
        web.evaluateJavaScript("window.__gongReveal && window.__gongReveal(\(nowMillis()))",
                               completionHandler: nil)
    }

    private func fireFire(_ web: WKWebView) {
        web.evaluateJavaScript("window.__gongFire && window.__gongFire(\(nowMillis()))",
                               completionHandler: nil)
    }

    /// reveal 和页面加载完成谁先谁后是不定的：lead=0 时 reveal 几乎立刻发生，
    /// 那会儿 __gongReveal 还不存在，喊了也白喊。所以两边都要触发一次。
    func webView(_ web: WKWebView, didFinish navigation: WKNavigation!) {
        loaded.insert(ObjectIdentifier(web))
        if fired {
            fireFire(web) // JS 入口会先补 reveal
        } else if revealed {
            fireReveal(web)
        }
    }

    func userContentController(_ c: WKUserContentController, didReceive m: WKScriptMessage) {
        guard m.name == "done" else {
            let mark = opts.tag.isEmpty ? "" : "[\(opts.tag)]"
            FileHandle.standardError.write("gong-overlay\(mark)[js] \(m.body)\n".data(using: .utf8)!)
            return
        }
        guard !finished, m.frameInfo.isMainFrame,
              let source = m.webView, source === primaryWeb else { return }
        finished = true
        for timer in timers { timer.cancel() }
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

    /// 只检查前台应用的 layer 0 窗口边界，不读取窗口标题或请求录屏权限。
    private func fullscreenAppInFront() -> Bool {
        guard let frontmost = NSWorkspace.shared.frontmostApplication,
              let windows = CGWindowListCopyWindowInfo(
                [.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID
              ) as? [[String: Any]]
        else { return false }

        let displays = NSScreen.screens.compactMap { screen -> CGRect? in
            let key = NSDeviceDescriptionKey("NSScreenNumber")
            guard let number = screen.deviceDescription[key] as? NSNumber else { return nil }
            return CGDisplayBounds(CGDirectDisplayID(number.uint32Value))
        }
        let tolerance = 2.0
        return windows.contains { window in
            guard (window[kCGWindowOwnerPID as String] as? NSNumber)?.int32Value == frontmost.processIdentifier,
                  (window[kCGWindowLayer as String] as? NSNumber)?.intValue == 0,
                  let rawBounds = window[kCGWindowBounds as String] as? [String: NSNumber],
                  let bounds = CGRect(dictionaryRepresentation: rawBounds as CFDictionary)
            else { return false }

            return displays.contains { display in
                bounds.minX <= display.minX + tolerance &&
                bounds.minY <= display.minY + tolerance &&
                bounds.maxX >= display.maxX - tolerance &&
                bounds.maxY >= display.maxY - tolerance
            }
        }
    }
}

let app = NSApplication.shared
let delegate = AppDelegate()
app.delegate = delegate
app.run()
