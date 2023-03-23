{}:

import ./nix {
	action = "shell";
	buildInputs = pkgs: with pkgs; [
		(writeShellScriptBin "staticcheck" "") # too slow
	];
}
