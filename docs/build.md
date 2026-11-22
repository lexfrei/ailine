# Building and testing

The application shares TypeScript sources between the standalone CLI and native Claude Code plugin. GoReleaser 2.20.2 packages CLI executables built with Bun 1.4.5. Python 3.10+ is used only for build-time archive verification and extraction, through its standard library. Neither Python nor a JavaScript runtime is required by an installed CLI executable.

## Prepare the compiler

Run from the repository root. The preparation script refuses an existing output directory; reuse the prepared directory for subsequent builds rather than running it again.

```bash
python3 -I tools/prepare_bun.py "$PWD/runtimes.bun-build" --native-only
export BUN_RUNTIME_DIR="$PWD/runtimes.bun-build"
export PATH="$BUN_RUNTIME_DIR:$PATH"
bun install --frozen-lockfile --ignore-scripts
```

The script verifies each ZIP against the reviewed release's pinned SHA-256 before reading its exact executable member. These hashes were checked against Bun's signed checksums during dependency review. It does not extract arbitrary archive paths or overwrite existing executables. See [dependency review](dependencies.md).

Omit `--native-only` in a fresh preparation directory to prepare all six targets for a complete release snapshot. `--archives DIRECTORY` can use previously downloaded official ZIPs; the same checksum validation applies.

## Develop and test

```bash
bun cmd/ailine/main.ts --help
bun run typecheck
bun run lint
bun run test
```

The complete test command also needs Claude Code 2.1.327 and GoReleaser 2.20.2. Native tests run through the actual host test runner. Build tests produce and execute a standalone CLI in a temporary directory, with no Bun, Node or ailine on PATH and conflicting autoload files beside it.

## Build a standalone executable

```bash
runtime_target=$(readlink "$BUN_RUNTIME_DIR/bun")
TARGET="${runtime_target#bun-}" goreleaser build --snapshot --single-target --id ailine-standalone --output ./ailine
```

The output path is ignored by Git. Choose another path if an executable there must be preserved. Version and commit are embedded by GoReleaser. The compiler disables runtime autoload of dotenv, bunfig, tsconfig and package.json files. The executable does not install packages at startup.

## Package without publishing

With all six runtime targets prepared:

```bash
goreleaser release --snapshot --skip=publish
```

The snapshot contains four macOS/glibc archives, two separate Linux musl archives, checksums, glibc Debian packages, the transitional `claudeline` package, and a Homebrew formula. The primary package owns the `claudeline` symlink so removing the transitional package does not remove the command alias. Musl archives require the platform's `libgcc` and `libstdc++`; system libraries are not copied into the release.

The existing formula publisher uses GoReleaser's deprecated `brews` configuration. GoReleaser 2.20.2 still generates it, but `goreleaser check` returns exit status 2 for this deprecation. Moving existing formula users to a cask is a separate distribution change.

## Embedded runtime and rebuilding

Standalone binaries embed Bun and its linked components, including LGPL-licensed JavaScriptCore and TinyCC. Their licenses apply in addition to this application's BSD-3-Clause license. Archives and Debian packages include the vendored JavaScript dependency notices and the pinned [Bun license inventory](https://github.com/oven-sh/bun/blob/bun-v1.4.5/LICENSE.md), which identifies component licenses and the upstream JavaScriptCore source/build instructions.

The application's TypeScript source and vendored JavaScript are available in the [source repository](https://github.com/lexfrei/ailine). Check out the tag matching the executable. To rebuild with a modified runtime, build a compatible Bun 1.4.5 executable using the upstream instructions, place it in a separate runtime directory under the corresponding `bun-<target>` filename, and set `BUN_RUNTIME_DIR` to that directory before running GoReleaser. The build uses that executable through `--compile-executable-path`; the official-runtime preparation script is not required for a locally modified runtime.
