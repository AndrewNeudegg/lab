{
  description = "Homelab agent dev shell";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-25.11";
    llm-agents.url = "github:numtide/llm-agents.nix";
  };

  outputs = { self, nixpkgs, llm-agents }:
    let
      system = "x86_64-linux";
      pkgs = import nixpkgs {
        inherit system;
        config.allowUnfree = true;
      };
      agents = llm-agents.packages.${system};
    in {
      devShells.${system}.default = pkgs.mkShell {
        packages = [
          pkgs.go
          pkgs.gopls
          pkgs.git
          pkgs.bun
          pkgs.chromium
          pkgs.podman
          pkgs.podman-compose
          agents.claude-code
          agents.codex
          agents.gemini-cli
        ];
        shellHook = ''
          export CHROME_BIN="${pkgs.chromium}/bin/chromium"
          export BROWSER="$CHROME_BIN"
        '';
      };
    };
}
