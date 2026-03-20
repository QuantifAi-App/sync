# Homebrew formula for quantifai-sync — the Quantifai telemetry sync agent.
# Maintained in-tree; published to a Homebrew tap on release.
#
# To install from tap: brew install quantifai-app/tap/quantifai-sync
# To install from local formula: brew install --formula packaging/homebrew/quantifai-sync.rb
class QuantifaiSync < Formula
  desc "Telemetry sync agent for Quantifai — streams Claude Code usage to your dashboard"
  homepage "https://quantifai.app"
  version "VERSION"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/quantifai-app/sync/releases/download/vVERSION/quantifai-sync-darwin-arm64.tar.gz"
      sha256 "SHA256_DARWIN_ARM64"
    else
      url "https://github.com/quantifai-app/sync/releases/download/vVERSION/quantifai-sync-darwin-amd64.tar.gz"
      sha256 "SHA256_DARWIN_AMD64"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/quantifai-app/sync/releases/download/vVERSION/quantifai-sync-linux-arm64.tar.gz"
      sha256 "SHA256_LINUX_ARM64"
    else
      url "https://github.com/quantifai-app/sync/releases/download/vVERSION/quantifai-sync-linux-amd64.tar.gz"
      sha256 "SHA256_LINUX_AMD64"
    end
  end

  def install
    bin.install "quantifai-sync"
  end

  service do
    run [opt_bin/"quantifai-sync", "run"]
    keep_alive true
    log_path var/"log/quantifai-sync.log"
    error_log_path var/"log/quantifai-sync.log"
  end

  def post_install
    ohai "Run 'quantifai-sync install' to register the background service"
    ohai "Or use 'brew services start quantifai-sync' to manage via Homebrew"
  end

  test do
    assert_match "quantifai-sync", shell_output("#{bin}/quantifai-sync version")
  end
end
