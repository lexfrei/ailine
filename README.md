# claudeline

[![CI](https://github.com/lexfrei/claudeline/actions/workflows/ci.yaml/badge.svg)](https://github.com/lexfrei/claudeline/actions/workflows/ci.yaml)
[![Go](https://img.shields.io/github/go-mod/go-version/lexfrei/claudeline)](https://go.dev/)
[![License](https://img.shields.io/github/license/lexfrei/claudeline)](LICENSE)

Real-time statusline for [Claude Code](https://docs.anthropic.com/en/docs/claude-code) showing live quota usage directly from stdin data.

## Example output

```text
🤖 Opus 4.7 ⏫💭 | 🧠 67% | 🔄 2 | 🟡 7d: 42% (4d 2h) | 🔴 5h: 91% (27m) | 🐙 lexfrei/claudeline 📝 #19 🌳 feat-api 🌿 feat/api
```

## Segments

| Segment | Description |
| --- | --- |
| 🤖 Model | Active model name, with effort / thinking / fast-mode indicators (Claude Code v2.1.119+) |
| 🐙 Repo | Repository host icon, `owner/name`, optional `#PR <state>` (`!MR` for GitLab merge requests, v2.1.234+), optional `🌳 worktree` (linked worktrees only) and `🌿 branch` (Claude Code v2.1.145+) |
| 🌳 Worktree / 🌿 Branch | Linked-worktree directory name and current branch; branch alone falls back here when no repository info is available |
| 💰 Cost | Cumulative session cost in USD (hidden by default for subscribers, see [Cost mode](#cost-mode)) |
| ⚠️/🔶/🔴 Status | Anthropic platform status: ⚠️ degraded, 🔶 major outage, 🔴 critical (hidden when all clear) |
| 🧠 Context | Context window usage percentage (color-coded) |
| 🧊 Prompt cache | Shown only when the prompt cache has gone cold, naming the session's most recent cache miss (Claude Code v2.1.251+, cause requires v2.1.260+), see [Prompt cache](#prompt-cache) |
| 🔄 Compactions | Number of context compactions in current session |
| 🟢/🟡/🟠/🔴 7d | 7-day rolling quota utilization with time until reset |
| 🟢/🟡/🟠/🔴 5h | 5-hour rolling quota utilization with time until reset |
| 🟢/🟡/🟠/🔴 7d-&lt;model&gt; | 7-day quota of the model in use, labelled after the bucket the server reports, e.g. `7d-fable` (requires [`--mac-insecure`](#advanced---mac-insecure-mode)) |

### Model indicators

  - effort `low` → `⬇️`, `medium` → no indicator, `high` → `⬆️`, `xhigh` → `⏫`, `max` → `🚀`
  - thinking enabled → `💭`
  - fast mode → `⚡`

### Prompt cache

The segment stays hidden while the cache is warm, which is the normal state. It appears when the cached prefix has gone cold, because the next request then re-processes the whole conversation instead of reading it back.

The word next to `🧊` names the session's most recent miss, which is usually an older event than the cold state itself. A miss rewrites the cache, so the prefix reads warm again on the request that missed, and what leaves it cold afterwards is sitting idle past its lifetime — on that path the miss is at least one lifetime old. The other way in is a response that carries no cache tokens at all, and there the miss can be seconds old. Either way, read the word as "this is what broke the cache", not "this is what is keeping it cold". Several causes collapse to the first plus `+`.

| Label | Miss attributed to |
| --- | --- |
| `ttl` | The cache sat idle past its 5m or 1h lifetime |
| `tools` | Tool definitions changed, for example an MCP server connected mid-session |
| `prompt` | The system prompt changed |
| `model` | The model changed |
| `effort` | The effort level changed |
| `fast` | Fast mode was toggled |
| `auto` | Auto mode was toggled |
| `history` | Earlier messages changed |
| `limits` | The usage-limit state changed, such as moving onto usage credits |
| `scope` | The cache scope or TTL changed |
| `betas` | Beta headers changed |
| `request` | Other request fields changed |
| `defer` | Deferred tool loading changed |
| `server` | Nothing local changed — likely server-side |

A cause Claude Code adds later shows up under its own name. When no cause could be identified, the segment is the bare `🧊`, or `cache: cold` under the `text` theme.

Requires Claude Code v2.1.251 for the cache state and v2.1.260 for the cause. On older versions, and before the session's first API response, the segment stays hidden.

### Themes

The icon style is selectable with `theme` in config or `--theme` on the CLI:

  - `emoji` (default) — the rendering shown above.
  - `text` — drops every emoji icon. Where an emoji encoded status by color (the `🟢/🟡/🟠/🔴` rate circles, the context meter, a changes-requested PR, and platform-status severity — `⚠️` minor → yellow, `🔶` major → orange, `🔴` critical → red), that color is carried onto the segment's text instead. Identifying emoji (`🤖`, `🐙`, `📝`) are removed, since the text already names the segment. The prompt cache segment is the exception: its text is a bare cause word, so it names itself under this theme and renders as `cache: tools` rather than `tools`.

Two kinds of state have no text form and are unavailable in this theme: the model's effort / thinking / fast-mode markers (`⏫`/`💭`/`⚡`) disappear entirely, and every PR review state except changes-requested (which survives as red) collapses to a plain `#N` (or `!N` for a merge request).

The same state as the example above, under `--theme text` (status shown here in **bold** to stand in for color):

```text
Opus 4.7 | **67%** | 2 | **7d: 42% (4d 2h)** | **5h: 91% (27m)** | lexfrei/claudeline #19 feat-api feat/api
```

### Auto-wrap on narrow terminals

When Claude Code exports `$COLUMNS` (v2.1.153+) and the joined statusline exceeds the available width, segments overflow onto additional lines instead of running past the host's line budget. Segments are never split mid-content — a single oversized segment lands on its own line.

A small safety margin (2 cells) is subtracted from `$COLUMNS` as a buffer: the terminal may have resized between Claude Code reading `$COLUMNS` and the script running, and empirically rows exactly equal to `$COLUMNS` were observed to drop their rightmost character. Tuning this further is out of scope until someone reports a concrete miss.

When `$COLUMNS` is unset (older Claude Code, non-terminal hosts), output stays on a single line — current behavior is preserved.

### Repo segment

Renders when Claude Code reports `workspace.repo` (a git remote pointing at a known host):

  - `🐙` github.com, `🦊` gitlab.com, `🪣` bitbucket.org, `📦` other hosts (with `host/` prefix)
  - review state followed by `#N`, or `!N` when Claude Code reports the change as a GitLab merge request: `📝` draft, `👀` pending, `💬` commented, `🔴` changes requested, `✅` approved
  - `🌳 worktree` — directory name of the linked worktree, shown only when `cwd` is a linked worktree (in the main clone it would just duplicate the repo name, so it is omitted)
  - `🌿 branch` — current branch read directly from `cwd/.git/HEAD`; when HEAD is detached or unreadable it falls back to the worktree name from stdin, but only if the `🌳` marker is not already shown (so the same name is never printed twice)

When no `workspace.repo` is present (non-git directory), the segment falls back to the bare `🌳 worktree 🌿 branch` form showing the same sources.

The repository and the PR number are also OSC 8 hyperlinks, so a click opens them in the browser. `hyperlinks = "auto"` (the default) emits them on terminals known to render OSC 8: iTerm2, WezTerm, Ghostty, kitty, Alacritty, foot, mintty, VS Code, Windows Terminal, Warp, Hyper, Tabby, rio, DomTerm, and recent VTE or Konsole builds. Terminal.app is not one of them. Set `hyperlinks = "true"` to force them on, `"false"` to turn them off. Inside tmux the links stay off even on a listed terminal: Claude Code drops OSC 8 coming from a statusline command there, so a link would render and never open. screen and zellij are treated the same way, on the assumption that the same rendering path applies. The PR link uses the URL Claude Code sends; the repository link is built as `https://host/owner/name`, so a self-hosted forge served over plain HTTP will not match.

Quota indicators compare your usage rate against elapsed time to warn about hitting limits:

- 🟢 usage pace is sustainable
- 🟡 usage is slightly ahead of schedule
- 🟠 usage is significantly ahead of schedule
- 🔴 on track to hit the limit before reset

### Cost mode

The cost segment has three modes:

- `auto` (default) — hide for Claude.ai subscribers (who have rate limits), show for API users
- `true` — always show
- `false` — never show

Set via `--cost auto|true|false` or `cost = "auto"` in config.

## Requirements

- Claude Code v2.1.82+ (provides `rate_limits` in statusline stdin)
- Claude Code v2.1.97+ recommended (adds `workspace.git_worktree` and `refreshInterval`)
- Claude Code v2.1.119+ enables effort, thinking, and fast-mode indicators
- Claude Code v2.1.145+ enables the combined repo / PR segment
- Claude Code v2.1.153+ enables terminal-width-aware line wrapping (via the `COLUMNS` env var)
- Claude Code v2.1.234+ reports GitLab merge requests, rendered as `!N`
- Current branch in `🌿 branch` is read directly from `cwd/.git/HEAD`, no `git` binary required

## Installation

### Homebrew

```bash
brew install lexfrei/tap/claudeline
```

### APT (Debian, Ubuntu)

```bash
curl --fail --silent --show-error --location https://apt.lexfrei.dev/lexfrei.asc \
  | sudo gpg --dearmor --output /usr/share/keyrings/lexfrei.gpg

sudo tee /etc/apt/sources.list.d/lexfrei.sources >/dev/null <<'EOF'
Types: deb
URIs: https://apt.lexfrei.dev
Suites: stable
Components: main
Signed-By: /usr/share/keyrings/lexfrei.gpg
EOF

sudo apt update && sudo apt install claudeline
```

Packages are built for `amd64` and `arm64`. The repository is [lexfrei/apt](https://github.com/lexfrei/apt).

### mise

```bash
mise use -g aqua:lexfrei/claudeline
```

### From source

```bash
go install github.com/lexfrei/claudeline/cmd/claudeline@latest
```

## Usage

Add the `statusLine` block to `~/.claude/settings.json`:

```json
{
  "statusLine": {
    "type": "command",
    "command": "claudeline",
    "padding": 0
  }
}
```

Claude Code pipes session data as JSON to stdin. claudeline reads it and outputs a formatted statusline string.

Restart Claude Code after changing settings.

### Keep quota timers ticking

By default Claude Code re-runs the statusline command on each new assistant message, on permission mode changes, and on vim mode toggles. While the session is idle (for example waiting on background subagents), the command is not re-run and quota reset timers freeze on screen.

Set `refreshInterval` in `~/.claude/settings.json` to also re-run the command on a fixed timer. This **adds** to event-driven updates, it does not replace them. Requires Claude Code v2.1.97+.

```json
{
  "statusLine": {
    "type": "command",
    "command": "claudeline",
    "padding": 0,
    "refreshInterval": 5
  }
}
```

## Configuration

Optional config file at `~/.claudelinerc.toml`:

```toml
theme = "emoji" # or "text"
hyperlinks = "auto" # or "true" / "false"

[segments]
model = true
effort = true
thinking = true
fast_mode = true
repo = true
worktree = true
cost = "auto"
status = true
context = true
prompt_cache = true
compactions = true
quota = true
per_model_quota = "auto" # only takes effect with mac_insecure

[cache]
status_ttl = "15s"
```

Set any segment to `false` to hide it (`cost` and `per_model_quota` accept `"auto"`, `"true"`, `"false"`). The two top-level keys take their own values: `theme` accepts `"emoji"` or `"text"`, `hyperlinks` accepts `"auto"`, `"true"` or `"false"`.

Run `claudeline validate --config ~/.claudelinerc.toml` to check your config for typos and invalid values.

### CLI flags

Flags override config file settings:

```bash
claudeline --cost false --no-status
claudeline --config /path/to/config.toml
```

Available flags: `--theme`, `--hyperlinks`, `--no-model`, `--no-effort`, `--no-thinking`, `--no-fast-mode`, `--no-repo`, `--no-worktree`, `--cost`, `--no-status`, `--no-context`, `--no-prompt-cache`, `--no-compactions`, `--no-quota`, `--mac-insecure`, `--per-model-quota=auto|true|false`, `--no-credits`. The last two only take effect with `--mac-insecure`.

`--per-model-quota` is the one flag whose value must be attached with `=` (a bare `--per-model-quota` keeps its old meaning, "every window", which rules out the space form). See [Per-model quota modes](#per-model-quota-modes).

## Advanced: `--mac-insecure` mode

For additional data not available in stdin, claudeline can access the Anthropic usage API directly via macOS Keychain. This gives you:

- Per-model 7-day quotas, including the buckets the API reports per model (Opus, Sonnet, Fable, …)
- Extra credit usage (💳 segment)

Stdin carries only the account-wide 5-hour and 7-day windows, so per-model quotas are available in this mode alone.

**Security note:** this mode reads your OAuth token from macOS Keychain. Only enable it if you understand the implications.

```bash
claudeline --mac-insecure
```

```toml
mac_insecure = true

[segments]
per_model_quota = "auto"
credits = true

[cache]
usage_ttl = "10m"
```

**Known limitations of `--mac-insecure`:** the Anthropic usage API has a very low rate limit (~5 requests per window). Responses are cached for 10 minutes by default (`usage_ttl`). macOS only.

### Per-model quota modes

`per_model_quota` selects which per-model windows are shown:

| Mode | Behavior |
| --- | --- |
| `auto` (default) | Only the window of the model in use — selecting Fable shows `7d-fable`, selecting Opus shows `7d-opus`. The label follows the bucket the server reports, so a finer bucket renders under its own name (`7d-sonnet-4-5`) |
| `true` | Every window the API reports |
| `false` | No per-model windows |

**The default changed:** per-model quotas used to be off unless explicitly enabled. They now default to `auto`, so a `--mac-insecure` statusline gains a `7d-<model>` segment whenever the API reports a quota for the model in use. Set `per_model_quota = false` to restore the previous behavior.

On the command line the value must be attached with `=`: `--per-model-quota=auto`, `--per-model-quota=true`, `--per-model-quota=false`. A bare `--per-model-quota` still means "every window", as it always did.

Model buckets are named by the server, so a quota for a newly released model appears without a claudeline update.

## License

[BSD-3-Clause](LICENSE)
