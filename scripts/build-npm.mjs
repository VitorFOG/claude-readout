#!/usr/bin/env node
// Build the platform binaries and assemble the npm packages under dist/npm.
//
//   node scripts/build-npm.mjs            build every target
//   node scripts/build-npm.mjs --check    only verify package.json agrees with the matrix
//   node scripts/build-npm.mjs --publish  publish what a previous build left under dist/npm
//
// The root package lists each platform package as an optional dependency at the
// same version; --check fails when that list and this matrix drift apart.
// --publish does not rebuild: it ships the tree the install smoke just ran, so
// the published files are the tested files. It skips any package already on npm
// at this version, so a release that failed halfway can be run again, and sets
// CLAUDE_READOUT_PUBLISH for the root package's prepublishOnly gate, which
// refuses a bare `npm publish`.
import { execFileSync } from "node:child_process";
import { cpSync, existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const dist = join(root, "dist", "npm");
const pkg = JSON.parse(readFileSync(join(root, "package.json"), "utf8"));

const targets = [
  { platform: "darwin", arch: "arm64", goos: "darwin", goarch: "arm64" },
  { platform: "darwin", arch: "x64", goos: "darwin", goarch: "amd64" },
  { platform: "linux", arch: "arm64", goos: "linux", goarch: "arm64" },
  { platform: "linux", arch: "x64", goos: "linux", goarch: "amd64" },
  { platform: "win32", arch: "arm64", goos: "windows", goarch: "arm64" },
  { platform: "win32", arch: "x64", goos: "windows", goarch: "amd64" },
];

const packageName = (t) => `claude-readout-${t.platform}-${t.arch}`;

function check() {
  const expected = Object.fromEntries(targets.map((t) => [packageName(t), pkg.version]));
  const actual = pkg.optionalDependencies ?? {};
  const problems = [];
  for (const [name, version] of Object.entries(expected)) {
    if (actual[name] !== version) problems.push(`${name} should be "${version}", is ${JSON.stringify(actual[name])}`);
  }
  for (const name of Object.keys(actual)) {
    if (!(name in expected)) problems.push(`${name} is not a build target`);
  }
  if (problems.length > 0) {
    console.error(`package.json optionalDependencies disagree with the build matrix:\n  ${problems.join("\n  ")}`);
    process.exit(1);
  }
  console.log(`package.json lists all ${targets.length} platform packages at ${pkg.version}`);
}

function build() {
  rmSync(dist, { recursive: true, force: true });
  for (const t of targets) {
    const dir = join(dist, packageName(t));
    const binary = join(dir, "bin", t.platform === "win32" ? "claude-readout.exe" : "claude-readout");
    mkdirSync(dirname(binary), { recursive: true });
    execFileSync("go", ["build", "-trimpath", "-ldflags", "-s -w", "-o", binary, "."], {
      cwd: root,
      stdio: "inherit",
      env: { ...process.env, CGO_ENABLED: "0", GOOS: t.goos, GOARCH: t.goarch },
    });
    writeFileSync(
      join(dir, "package.json"),
      `${JSON.stringify(
        {
          name: packageName(t),
          version: pkg.version,
          description: `The ${t.platform}-${t.arch} binary for claude-readout.`,
          license: pkg.license,
          repository: pkg.repository,
          os: [t.platform],
          cpu: [t.arch],
          files: ["bin"],
        },
        null,
        2,
      )}\n`,
    );
    writeFileSync(
      join(dir, "README.md"),
      `# ${packageName(t)}\n\nThe ${t.platform}-${t.arch} binary for [claude-readout](https://github.com/VitorFOG/claude-readout). Install \`claude-readout\` instead of this package.\n`,
    );
    cpSync(join(root, "LICENSE"), join(dir, "LICENSE"));
    console.log(`built ${packageName(t)}`);
  }
}

function alreadyPublished(name, version) {
  try {
    const out = execFileSync("npm", ["view", `${name}@${version}`, "version"], { stdio: ["ignore", "pipe", "ignore"] });
    return out.toString().trim() !== "";
  } catch {
    return false;
  }
}

function publish() {
  const args = ["publish", "--access", "public", "--provenance"];
  const env = { ...process.env, CLAUDE_READOUT_PUBLISH: "1" };
  const packages = [...targets.map((t) => [packageName(t), join(dist, packageName(t))]), [pkg.name, root]];
  for (const [name, cwd] of packages) {
    if (alreadyPublished(name, pkg.version)) {
      console.log(`${name}@${pkg.version} is already on npm, skipping`);
      continue;
    }
    execFileSync("npm", args, { cwd, stdio: "inherit", env });
  }
}

check();
if (process.argv.includes("--check")) process.exit(0);
if (process.argv.includes("--publish")) {
  if (!existsSync(dist)) {
    console.error(`nothing to publish: ${dist} is missing, run the build first`);
    process.exit(1);
  }
  publish();
} else {
  build();
}
