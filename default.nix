{ buildGoModule }:

buildGoModule rec {
  pname = "genicam-device-plugin";
  version = "0.0.1";

  src = ./.;

  subPackages = [ "." ];
  modSha256 = "0cnxbni5g8fkcii9i4dg8v1wjcby899l20lsmx4nlxq1ai9q91v9";
}
