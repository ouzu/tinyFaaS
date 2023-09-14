{
  description = "tinyFaaS-cli";

  inputs.flake-utils.url = "github:numtide/flake-utils";

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let pkgs = nixpkgs.legacyPackages.${system};
      in {
        devShell = import ./shell.nix { inherit pkgs; };
        packages = {
          tinyFaaS = import ./package.nix { inherit pkgs; };
          default = self.packages.${system}.tinyFaaS;
        };
      });
}
