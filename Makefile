VSCODE := editors/vscode

.PHONY: vs

# vs: package the VS Code extension into editors/vscode/loom-lang-<version>.vsix.
#   Dependencies (vsce, and the tokenizer used by the test) are downloaded on first use,
#   pinned by package-lock.json.
#   The tokenizer test runs first: a grammar that mis-scopes a keyword should not ship.
#   --ignore-scripts: vsce's signing dependency downloads platform binaries at install time
#   and can hang there; local packaging never signs, so the download is not needed.
vs:
	@command -v npm >/dev/null 2>&1 || { echo "⛔ make vs needs Node.js (npm): install it from https://nodejs.org and rerun"; exit 1; }
	@cd $(VSCODE) && \
	{ [ -x node_modules/.bin/vsce ] && [ -d node_modules/vscode-textmate ] || \
	  { echo "↓ downloading the vsce packager and test dependencies (first run)"; npm ci --no-audit --no-fund --ignore-scripts; }; } && \
	npm test && \
	./node_modules/.bin/vsce package
