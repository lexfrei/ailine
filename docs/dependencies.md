# Dependency review

Reviewed on 2026-11-21 for the TypeScript migration. Publisher history and artifact origin are the trust criteria; stars and download counts are not. This review covers the exact versions below, not future semver-compatible releases. It does not establish freedom from vulnerabilities.

## Runtime packages

| Package | Version | Publisher and evidence |
| --- | --- | --- |
| `smol-toml` | 1.9.1 | `cyyynthia` maintains `squirrelchat/smol-toml`; recent releases use GitHub Actions trusted publishing. The selected tarball passed Sigstore verification restricted to the repository, publish workflow, tag and source commit. |
| `string-width` | 9.0.0 | `sindresorhus` owns and publishes the project, with continuous registry history since 2015. The packaged JavaScript matches the pinned source commit. No source-bound attestation is advertised. |
| `strip-ansi` | 7.2.0 | Current sole maintainer and publisher is `sindresorhus`. The packaged JavaScript matches the pinned source commit. A former publisher account was compromised in 2025; see below. |
| `ansi-regex` | 6.6.0 | Current sole maintainer and publisher is `sindresorhus`. The packaged JavaScript matches the pinned source commit. A former publisher account was compromised in 2025; see below. |
| `get-east-asian-width` | 1.7.0 | `sindresorhus` owns and publishes the project, with continuous registry history since 2023. All packaged JavaScript matches the pinned source commit. No source-bound attestation is advertised. |

Only the TOML parser and string-width library are direct runtime dependencies. The other three are pinned transitively through overrides and the lockfile. None declares an install, preinstall, postinstall or prepare hook. The plugin bundle includes their readable code and must carry their license notices; users do not install npm packages to run the plugin.

The [`smol-toml` attestation](https://registry.npmjs.org/-/npm/v1/attestations/smol-toml@1.9.1) was cryptographically verified against source commit `69aa050fef018b71799519bb2cbb54f158a0c5f9`, `refs/tags/v1.9.1`, and the certificate identity `https://github.com/squirrelchat/smol-toml/.github/workflows/publish.yaml@refs/tags/v1.9.1`. Verification required the GitHub OIDC issuer and rejected self-hosted runners. Historical parser DoS advisories were fixed before this version; malformed input, excessive nesting and prototype-key behavior still require regression coverage.

The width-chain JavaScript was compared byte for byte with these source commits:

- [`string-width`](https://github.com/sindresorhus/string-width/tree/3082bffe020ae22b11b7288bb831a9cf0fd82d69): `3082bffe020ae22b11b7288bb831a9cf0fd82d69`.
- [`strip-ansi`](https://github.com/chalk/strip-ansi/tree/38ff9f2282540422031ed523f0060c7bb575e20f): `38ff9f2282540422031ed523f0060c7bb575e20f`.
- [`ansi-regex`](https://github.com/chalk/ansi-regex/tree/7e21a3e137887b61c7f4148992c220548577ba3a): `7e21a3e137887b61c7f4148992c220548577ba3a`.
- [`get-east-asian-width`](https://github.com/sindresorhus/get-east-asian-width/tree/4d326374582406bdd9b430f992423a8c63dbadda): `4d326374582406bdd9b430f992423a8c63dbadda`.

Registry integrity, source-byte comparison and signed build provenance establish different things. The four width packages have integrity metadata but no advertised source-bound attestations. The comparison confirms the selected package contents match the reviewed source; it is not a substitute for a signed build statement.

## Publisher incident history

The former `qix` npm account was compromised on 2025-09-08, affecting `strip-ansi@7.1.1` and `ansi-regex@6.2.1`. The selected versions are later releases from the current publisher. Primary records document removal of the compromised account and clean replacement releases: [debug-js/debug#1005](https://github.com/debug-js/debug/issues/1005#issuecomment-3266868187) and [chalk/chalk#656](https://github.com/chalk/chalk/issues/656#issuecomment-3266848902).

That incident is part of the assessment, not evidence that the selected versions are malicious or that a different publisher account was compromised. Shared ownership concentrates account risk; it does not create independent trust boundaries between these packages.

## Build tools and types

- Bun 1.4.5 comes from the official `oven-sh/bun` release published by `robobun`. The installed runtime matched the official artifact. The six runtime ZIPs used for platform probes also matched checksums whose signature passed verification with asserted publisher key `F3DCC08A8572C0749B3E18888EAB4D40A7B22B59`. The key is bootstrapped from the pinned [upstream Containerfile](https://github.com/oven-sh/bun/blob/bun-v1.4.5/dockerhub/alpine/Dockerfile), not from a locally established Web-of-Trust identity.
- TypeScript 7.0.2 is published by `microsoft1es` from Microsoft's official [release pipeline](https://github.com/microsoft/TypeScript/blob/main/tools/pipelines/typescript-publish.yml). The main package and selected Darwin arm64 compiler tarball matched the digests of the official GitHub release assets. No npm source-bound attestation is advertised for those packages.
- Biome 2.6.8 is maintained by `biomejs` under public governance and published through GitHub Actions. The main package and selected Darwin arm64 CLI package passed Sigstore verification restricted to the repository and its release workflow identity.
- `@types/bun@1.4.5` and `@types/node@26.6.6` are published by DefinitelyTyped's `types` account through [Microsoft's publishing tooling](https://github.com/microsoft/DefinitelyTyped-tools/tree/master/packages/publisher). `bun-types@1.4.5` comes from `oven-sh/bun`; `undici-types@8.9.0` comes from `nodejs/undici` and passed release-workflow provenance verification. These are development-only dependencies.

Publisher metadata and lifecycle hooks were checked for all 30 optional TypeScript/Biome platform packages. That metadata check is not cryptographic verification of every uninstalled binary. Each selected platform artifact must be verified before its first execution in the corresponding build environment. Upstream wildcard type dependencies are constrained by exact overrides. Package installation uses `--ignore-scripts`.

The reviewed lockfile returned no advisory findings on the review date. An empty advisory report does not replace publisher review, source inspection or artifact verification.

## Platform libraries

Standalone executables contain Bun; they do not require a separately installed JavaScript runtime. They still depend on platform libraries. The glibc and musl artifacts are separate. Alpine requires its signed `libstdc++` and `libgcc` packages, as the upstream Bun image does. The platform probe used official Alpine images and pinned GCC runtime packages from `alpinelinux/aports`, without unsigned packages or bundled system `.so` files.

Bun's x64 CPU minimum is SSE4.2. The runtime is part of the shipped executable, so a Bun security update requires rebuilding the CLI even if application code is unchanged.

## Updates

A dependency update must repeat the publisher, incident, source and artifact checks for its new resolved versions. A version bump must not silently restore floating transitive dependencies or enable lifecycle scripts. Preserve the lockfile, provenance checks and license material, and review newly introduced runtime or platform dependencies before accepting the update.
