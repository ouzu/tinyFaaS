{ pkgs ? import <nixpkgs> { } }:
with pkgs;
buildGoModule {
  name = "tinyFaaS";
  src = lib.cleanSource ./.;
  vendorSha256 = "sha256-Mqr8OpxxJ7q0V16+a+PzZotPjh5XJyyCNyYHkmER0ck=";
}