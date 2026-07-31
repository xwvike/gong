class Gong < Formula
  desc "I'm outta here!"
  homepage "https://github.com/xwvike/gong"
  version "0.1.7"
  license "MIT"

  url "https://github.com/xwvike/gong/releases/download/v#{version}/gong-#{version}-macos-universal.tar.gz"
  sha256 "785215819588aecc0c85809013034215f0b5cb2fbd96ca2f51250d8bd7356786"

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
