class Gong < Formula
  desc "I'm outta here!"
  homepage "https://github.com/xwvike/gong"
  version "0.1.17"
  license "MIT"

  url "https://github.com/xwvike/gong/releases/download/v#{version}/gong-#{version}-macos-universal.tar.gz"
  sha256 "a93eee8a99f1b16108f85aee09ae956b6caa511a2b2f379cdb335fe32772423a"

  depends_on :macos

  def install
    bin.install "gong"
    bin.install "gong-overlay"
    pkgshare.install "themes"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gong version")
    assert_match "led", shell_output("#{bin}/gong themes")
    system bin/"gong-overlay", "--force", "--timeout", "3",
           "--theme", pkgshare/"themes/led/index.html"
  end
end
