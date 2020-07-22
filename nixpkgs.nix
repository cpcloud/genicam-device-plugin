let
  overlay = self: super: {

    nomad = super.nomad.overrideAttrs (oldAttrs: rec {
     name = "nomad-${version}";
     version = "v0.12.0";
     src = super.fetchurl {
       url = "https://github.com/hashicorp/nomad/archive/${version}.tar.gz";
       sha256 = "133k442wzyh9k8s9jxriaczvliffw63p6s1csjbzf574zfwczsyj";
     };
    });

    genicam-device-plugin = super.callPackage ./default.nix {};
    plugin-launcher = let
    drv = { stdenv, buildGoPackage, nomad }:

      buildGoPackage rec {
        pname = "launcher";
        version = nomad.version;

        src = nomad.src;
        goPackagePath = "github.com/hashicorp/nomad";

        outputs = [ "out" ];

        buildPhase = ''
          cd go/src/${goPackagePath}
          go build -o bin/launcher plugins/shared/cmd/launcher/main.go
        '';

        installPhase = ''
          mkdir -pv $out/bin
          cp -v bin/launcher $out/bin/
        '';

        allowGoReference = true;

      };
    in super.callPackage drv {};
  };

in import <nixpkgs> { overlays = [ overlay ]; }
