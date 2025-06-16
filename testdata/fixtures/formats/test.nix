{ pkgs ? import <nixpkgs> {} }:

# NOTE: This is a sample Nix configuration file
let
  myVar = "hello world";
  
  # XXX: This needs to be fixed later
  brokenFunction = x: y: x + y + 1;
  
  /* NOTE: Multi-line comment
     that spans multiple lines
  */
  myPackage = pkgs.stdenv.mkDerivation {
    pname = "example";
    version = "1.0.0";
    
    # TODO: Add proper source
    src = ./.;
    
    buildPhase = ''
      echo "Building..."
    '';
  };
in
{
  inherit myPackage;
  
  # should not match in code
  xxx = "XXX";
}
