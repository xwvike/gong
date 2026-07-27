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
  version "0.1.1"
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

  def caveats
    <<~EOS
      装完还差一步——把定时交给 launchd：

        gong on

      默认两条：noon 12:00、evening 18:00，周一到周五。
      要增删改查、换主题、预览，跑 gong set。

      卸载前请先跑：

        gong off

      brew uninstall 不会清 ~/Library/LaunchAgents 里的 plist，formula 没有
      uninstall hook。不跑 gong off 的话，那几条定时会一直尝试拉起一个
      已经不存在的二进制，而且是静默失败。
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
