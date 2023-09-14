{ pkgs ? import <nixpkgs> { } }:
with pkgs;
mkShell {
  name = "tinyFaaS-shell";

  buildInputs = [
    go

    nixfmt

    tcpdump
    wireshark
  ];

  shellHook = ''
    # ...
  '';
}
