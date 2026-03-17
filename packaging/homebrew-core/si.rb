class Si < Formula
  desc "AI-first CLI for orchestrating coding agents and provider operations"
  homepage "https://github.com/Aureuma/si"
  url "https://github.com/Aureuma/si/archive/refs/tags/v0.54.0.tar.gz"
  sha256 "c67ee221ca3aade89595005aadfabf8fc8de829903d22359bef956cfd7e9dfa7"
  license "AGPL-3.0-only"
  head "https://github.com/Aureuma/si.git", branch: "main"

  depends_on "rust" => :build
  def install
    system "cargo", "install", "--locked", *std_cargo_args(path: "rust/crates/si-cli"), "--bin", "si-rs"
    mv bin/"si-rs", bin/"si"
  end

  test do
    output = shell_output("#{bin}/si version")
    assert_match "si version", output
  end
end
