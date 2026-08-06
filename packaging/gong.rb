class Gong < Formula
  desc "I'm outta here!"
  homepage "https://github.com/xwvike/gong"
  version "0.1.14"
  license "MIT"

  url "https://github.com/xwvike/gong/releases/download/v#{version}/gong-#{version}-macos-universal.tar.gz"
  sha256 "8a5b02a1baa7efa2104d3f8f5cde3d23cbe651c47565e19fa8e1525600498576"

  depends_on :macos

  def install
    bin.install "gong"
    bin.install "gong-overlay"
    pkgshare.install "themes"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gong version")
    assert_match "default", shell_output("#{bin}/gong themes")
    system bin/"gong-overlay", "--force", "--timeout", "3",
           "--theme", pkgshare/"themes/default/index.html"
  end
end
