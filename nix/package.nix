{
  lib,
  buildGoModule,
  version ? "dev",
}:
buildGoModule {
  pname = "lazify";
  inherit version;

  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../go.mod
      ../go.sum
      ../cmd
      ../internal
      ../examples
    ];
  };

  vendorHash = "sha256-UKZprsQskbhEb2rhHAs/YASOjKJTWfQ5ecXeDCy95D4=";
  subPackages = [ "cmd/lazify" ];

  # subPackages would limit the tests to cmd/lazify; run the whole suite.
  checkPhase = ''
    runHook preCheck
    go test ./...
    runHook postCheck
  '';

  meta = {
    description = "One declarative YAML file → a lazygit-style TUI";
    homepage = "https://github.com/martinhrvn/lazify";
    mainProgram = "lazify";
  };
}
