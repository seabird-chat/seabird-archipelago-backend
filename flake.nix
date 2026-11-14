{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

    flake-parts = {
      url = "github:hercules-ci/flake-parts";
      inputs.nixpkgs-lib.follows = "nixpkgs";
    };
  };

  outputs =
    inputs@{
      flake-parts,
      nixpkgs,
      ...
    }:
    flake-parts.lib.mkFlake { inherit inputs; } {
      systems = nixpkgs.lib.systems.flakeExposed;

      perSystem =
        { pkgs, ... }:
        let
          seabird-archipelago-backend = pkgs.buildGoModule rec {
            pname = "seabird-archipelago-backend";
            version = "0.1.0";
            src = ./.;
            vendorHash = "sha256-S8xXJSP31OpMA/gpZ9etPV6dqK4TBZC18zPJJ6qS8DA=";
            subPackages = [ "cmd/${pname}" ];
            ldflags = [
              "-s"
              "-w"
            ];
            env.CGO_ENABLED = 0;
          };
        in
        {
          devShells.default = pkgs.mkShell {
            packages = with pkgs; [
              go
              gopls
              gotools
              golangci-lint
              just
              treefmt
              nixfmt
            ];
          };

          packages = {
            default = seabird-archipelago-backend;
            inherit seabird-archipelago-backend;
          };
        };
    };
}
