with (import ./nixpkgs.nix);
mkShell {
  buildInputs = [ plugin-launcher ];
  inputsFrom = [ genicam-device-plugin ];
  shellHook = ''
    export GOPATH=$(mktemp -d)
    export GOBIN=$GOPATH/bin
    ln -s $GOBIN bin
  '';
}
