# CLAUDE.md

## Release workflow

Strict sequential flow, each step depends on the previous one succeeding.

### 1. PR

- Create feature branch from `master`
- Commit changes (small, focused commits with `--signoff`)
- Push branch, create draft PR via `gh pr create --draft`
- Wait for CI (Lint + Test + Build)

### 2. Merge

- Only after green CI
- `gh pr ready <number>` then `gh pr merge --squash --delete-branch`

### 3. Tag

- Determine version bump (semver): patch/minor/major
- Switch to `master`, pull latest
- Create **signed** tag: `git tag --sign vX.Y.Z --message "Release vX.Y.Z: description"`
- Verify signature: `git tag --verify vX.Y.Z`
- Push tag: `git push origin vX.Y.Z`

### 4. GoReleaser (automatic)

Tag push triggers CI release job:

- Lint → Test → Build → GoReleaser
- GoReleaser builds darwin + linux binaries for amd64 and arm64
- Creates GitHub release with binaries, `.deb` packages and checksums
- Updates Homebrew tap (`lexfrei/homebrew-tap`)
- The release job sends `lexfrei/apt` a dispatch, which rebuilds the apt repository with the new `.deb` files

### 5. Update release notes (manual)

GoReleaser creates release with auto-generated changelog (commit list). This is not enough. After GoReleaser finishes, update the release body with a human-readable description:

```bash
gh release edit vX.Y.Z --notes "$(cat <<'EOF'
## What's New

### Feature name

Description of what changed and why it matters to users.
Explain new flags, config options, behavior changes.

Include code examples for new options:

**CLI flag:**

```bash
ailine --new-flag
```

**Config file** (`~/.ailinerc.toml`):

```toml
[segments]
new_option = true
```

### Other improvements

- Bullet list of smaller changes
- Focus on user-visible behavior, not implementation details

**Full Changelog**: https://github.com/lexfrei/ailine/compare/vPREV...vX.Y.Z
EOF
)"
```

#### Release notes format

- **Title**: `vX.Y.Z` (just the version)
- **Body structure**:
  - `## What's New` — main heading
  - `### Feature name` — one section per significant change
  - User-facing description: what it does, how to enable, config examples
  - `### Other improvements` — bullet list for minor changes
  - `**Full Changelog**` link at the bottom
- **Tone**: technical, concise, user-focused
- **No internal details**: no commit hashes, no file paths, no refactoring details unless user-visible
- **Code examples**: include for any new CLI flags or config options
