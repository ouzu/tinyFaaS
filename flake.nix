{
  description = "tinyFaaS-cli";

  inputs.flake-utils.url = "github:numtide/flake-utils";

  inputs.tinyFaaS-cli.url = "github:ouzu/tinyFaaS-cli";
  inputs.tinyFaaS-cli.inputs.nixpkgs.follows = "nixpkgs";

  outputs = { self, nixpkgs, flake-utils, tinyFaaS-cli, ... }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
        tfcli = tinyFaaS-cli.packages.${system};
      in
      {
        devShell = import ./shell.nix { inherit pkgs; inherit tfcli; };
        packages = {
          tinyFaaS = import ./package.nix { inherit pkgs; };
          default = self.packages.${system}.tinyFaaS;
        };
        nixosModules.tinyFaaS = import ./module.nix { inherit pkgs; };
      });
}
