class Si < Formula
  desc "AI-first CLI for orchestrating coding agents and provider operations"
  homepage "https://github.com/Aureuma/si"
  url "https://github.com/Aureuma/si/archive/refs/tags/v0.48.0.tar.gz"
  sha256 "a09760ec0b221a644f22115f6a4879e1e575e5e02933233191bbbead6aa1cab8"
  license "AGPL-3.0-only"
  head "https://github.com/Aureuma/si.git", branch: "main"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w"), "./tools/si"
  end

  test do
    output = shell_output("#{bin}/si version")
    assert_match "si version", output
  end
end
