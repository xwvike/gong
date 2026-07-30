class Gong < Formula
  desc "I'm outta here!"
  homepage "https://github.com/xwvike/gong"
  version "0.1.6"
  license "MIT"

  url "https://github.com/xwvike/gong/releases/download/v#{version}/gong-#{version}-macos-universal.tar.gz"
  sha256 "e440f87db73a6b084336ce3f0fca863f723b61b78be2258c52255f2ee5d8d94e"

  depends_on :macos

  def install
    bin.install "gong"
    bin.install "gong-overlay"
    pkgshare.install "themes"
    doc.install "doc.md" if File.exist?("doc.md")
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gong version")
    assert_match "default", shell_output("#{bin}/gong themes")
    system bin/"gong-overlay", "--force", "--timeout", "3",
           "--theme", pkgshare/"themes/default/index.html"
  end
end
