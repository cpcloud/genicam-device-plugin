with (import ./nixpkgs.nix);
mkShell {
  buildInputs = [ plugin-launcher ];
  inputsFrom = [ genicam-device-plugin ];
}
