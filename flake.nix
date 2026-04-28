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
      playwrightLibs = with pkgs; [
        stdenv.cc.cc.lib
        glib
        gtk3
        pango
        atk
        cairo
        gdk-pixbuf
        alsa-lib
        freetype
        fontconfig
        dbus
        expat
        nss
        nspr
        libdrm
        libgbm
        libxkbcommon
        mesa
        udev
        xorg.libX11
        xorg.libXcomposite
        xorg.libXdamage
        xorg.libXext
        xorg.libXfixes
        xorg.libXrandr
        xorg.libXrender
        xorg.libXcursor
        xorg.libXi
        xorg.libxcb
      ];
    in {
      devShells.${system}.default = pkgs.mkShell {
        packages = [
          pkgs.go
          pkgs.gopls
          pkgs.git
          pkgs.bun
          pkgs.chromium
          pkgs.xvfb-run
          pkgs.podman
          pkgs.podman-compose
          agents.claude-code
          agents.codex
          agents.gemini-cli
        ];
        shellHook = ''
          export CHROME_BIN="${pkgs.chromium}/bin/chromium"
          export BROWSER="$CHROME_BIN"
          export LD_LIBRARY_PATH="${pkgs.lib.makeLibraryPath playwrightLibs}:''${LD_LIBRARY_PATH:-}"
        '';
      };
    };
}
