{ pkgs, tfcli, ... }:
with pkgs;
mkShell {
  name = "tinyFaaS-shell";

  buildInputs = [
    go

    nixfmt

    tcpdump
    wireshark

    grpc-tools
    protoc-gen-go
    protoc-gen-go-grpc

    tfcli.tinyFaaS-cli
  ];

  shellHook = ''
    # ...
  '';
}
