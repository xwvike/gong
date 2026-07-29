# 这份文件的部署目标是 github.com/xwvike/homebrew-tap 的 Formula/gong.rb
# （brew 里叫 xwvike/tap，是已有的 tap，不要为 gong 另建一个）。
# 放在源码里是为了跟版本一起管。发版时拷过去，把 sha256 换成 release 里那个。
#
# 三条不能破的规矩：
#   1. 必须是 formula，永远不要做成 cask。Gatekeeper 这轮收紧只影响 cask；
#      formula 下载不打 com.apple.quarantine，所以未签名的二进制也不弹框。
#   2. 装的时候【不许碰用户 home】。install 跑在沙箱里只准写 Cellar 前缀，
#      写 ~/Library/LaunchAgents 会被拦。plist 由 `gong on` 自己生成。
#   3. 什么都不用注入：gong 会按 bin/../share/gong/themes 找内置主题、
#      按同级目录找 gong-overlay，正好是 Homebrew 的标准布局。
class Gong < Formula
  desc "到点在所有屏幕最顶层播一段动画的定时提醒，不抢焦点、不吃点击"
  homepage "https://github.com/xwvike/gong"
  version "0.1.3"
  license "MIT"

  url "https://github.com/xwvike/gong/releases/download/v#{version}/gong-#{version}-macos-universal.tar.gz"
  sha256 "REPLACE_WITH_SHA256_FROM_RELEASE"

  depends_on :macos

  def install
    bin.install "gong"
    bin.install "gong-overlay"
    pkgshare.install "themes"
    doc.install "doc.md" if File.exist?("doc.md")
  end

  # caveats 是唯一会打到用户终端上的东西，只留「不说会出事」的两条：
  # 不跑 gong on 等于没装；直接 brew uninstall 会留下静默失败的 plist。
  # 默认定时、子命令、主题这些跑一次 gong 就全列出来了，不必在这里重说一遍。
  def caveats
    <<~EOS
      装完还差一步，把定时交给 launchd：  gong on

      卸载用 gong uninstall，别直接 brew uninstall——formula 没有 uninstall
      hook，plist 会留在 ~/Library/LaunchAgents 每天到点静默失败。
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gong version")
    # themes 会去解析内置主题目录，列得出来就说明 bin/../share/gong/themes 这条路对了
    assert_match "default", shell_output("#{bin}/gong themes")
    # --force 跳过时间窗，能跑完说明壳和主题都完好
    system bin/"gong-overlay", "--force", "--timeout", "3",
           "--theme", pkgshare/"themes/default/index.html"
  end
end
