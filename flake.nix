{
  description = "Baton - runtime-agnostic multi-agent orchestrator";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go_1_25
            golangci-lint
            goreleaser
          ];

          shellHook = ''
            echo "baton dev shell — go $(go version | cut -d' ' -f3), golangci-lint $(golangci-lint version --short 2>/dev/null || echo 'n/a')"
          '';
        };

        packages.default = pkgs.buildGoModule {
          pname = "baton";
          version = "dev";
          src = ./.;
          vendorHash = null;
          ldflags = [ "-s" "-w" "-X main.version=dev" ];
        };
      });
}
