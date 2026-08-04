class Gong < Formula
  desc "I'm outta here!"
  homepage "https://github.com/xwvike/gong"
  version "0.1.10"
  license "MIT"

  url "https://github.com/xwvike/gong/releases/download/v#{version}/gong-#{version}-macos-universal.tar.gz"
  sha256 "33a937ac0210a7c0fd21e269b551dbce086e4e89492fc9c7fb88e67e9636e002"

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
