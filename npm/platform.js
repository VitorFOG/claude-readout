"use strict";

// One npm package per platform carries the binary. The names follow the same
// `claude-readout-<platform>-<arch>` scheme as the build matrix in
// scripts/build-npm.mjs, whose --check only verifies package.json against that
// matrix; keep the scheme here in step by hand.
const packageName = `claude-readout-${process.platform}-${process.arch}`;
const binaryName = process.platform === "win32" ? "claude-readout.exe" : "claude-readout";

/** Absolute path of the platform binary, or null when its package is absent. */
function binaryPath() {
  try {
    return require.resolve(`${packageName}/bin/${binaryName}`);
  } catch {
    return null;
  }
}

module.exports = { packageName, binaryName, binaryPath };
