import { expect, test } from "bun:test";
import { emptyCompactionCache } from "../../plugin/core/compaction";
import { defaults } from "../../plugin/core/config";
import { emptyInput } from "../../plugin/core/input";
import {
	buildSegments,
	renderStatusline,
	type RenderRuntime,
} from "../../plugin/core/render";
import type { CacheKey, FileInfo } from "../../plugin/core/runtime";

const nowMs = Date.parse("2026-11-21T00:00:00Z");

function fixture() {
	const input = emptyInput();
	const config = defaults();
	config.segments.status = false;
	config.segments.compactions = false;
	config.segments.quota = false;
	config.segments.credits = false;
	config.segments.worktree = false;
	config.segments.repo = false;
	config.segments.cost = "false";
	const files = new Map<string, Uint8Array | "directory">();
	const cache = new Map<CacheKey, string>();
	const calls: string[] = [];
	const response = {
		status: 200,
		headers: {} as Record<string, string>,
		body: "{}",
	};
	const runtime: RenderRuntime = {
		now: () => nowMs,
		files: {
			async stat(path): Promise<FileInfo | undefined> {
				const value = files.get(path);
				if (value === undefined) return undefined;
				return {
					kind: value === "directory" ? "directory" : "file",
					size: value === "directory" ? 0 : value.length,
					mtimeMs: nowMs,
				};
			},
			async read(path, offset = 0, length) {
				const value = files.get(path);
				if (value === undefined || value === "directory") return undefined;
				return value.slice(
					offset,
					length === undefined ? undefined : offset + length,
				);
			},
		},
		cache: {
			async read(key) {
				return cache.get(key);
			},
			async write(key, value) {
				cache.set(key, value);
			},
		},
		async getToken() {
			calls.push("token");
			return undefined;
		},
		async hashToken() {
			return "test-hash";
		},
		async httpGet(url) {
			calls.push(url);
			return response;
		},
	};
	return { input, config, runtime, files, cache, calls, response };
}

const encode = (value: string) => new TextEncoder().encode(value);

for (const [effort, expected] of [
	["low", "⬇️"],
	["medium", ""],
	["high", "⬆️"],
	["xhigh", "⏫"],
	["max", "🚀"],
	["future", ""],
] as const) {
	test(`model qualifiers follow the model for effort ${effort}`, async () => {
		const { input, config, runtime } = fixture();
		input.model.displayName = "Opus";
		input.effortLevel = effort;
		input.thinking = true;
		input.fastMode = true;
		expect(
			await renderStatusline(input, config, runtime, {
				maxWidth: 0,
				hyperlinks: false,
			}),
		).toBe(`🤖 Opus ${expected}💭⚡`);
	});
}

test("text theme removes model qualifiers without leaving spaces", async () => {
	const { input, config, runtime } = fixture();
	config.theme = "text";
	input.model.displayName = "Opus";
	input.effortLevel = "max";
	input.thinking = true;
	input.fastMode = true;
	expect(await buildSegments(input, config, runtime, false)).toEqual(["Opus"]);
});

test("missing display name remains Claude rather than guessing from the ID", async () => {
	const { input, config, runtime } = fixture();
	input.model.id = "custom-provider-model";
	expect(await buildSegments(input, config, runtime, false)).toEqual([
		"🤖 Claude",
	]);
});

test("cost auto policy follows input window presence, not quota visibility", async () => {
	const { input, config, runtime } = fixture();
	config.segments.cost = "auto";
	input.costUsd = 0.125;
	expect(await buildSegments(input, config, runtime, false)).toEqual([
		"🤖 Claude",
		"💰 $0.12",
	]);
	input.rateLimits.fiveHour = { usedPercentage: 0, resetsAtMs: undefined };
	expect(await buildSegments(input, config, runtime, false)).toEqual([
		"🤖 Claude",
	]);
	config.segments.cost = "true";
	expect(await buildSegments(input, config, runtime, false)).toEqual([
		"🤖 Claude",
		"💰 $0.12",
	]);
});

for (const [writes, observed, warm, visible] of [
	[undefined, true, false, true],
	[1, true, false, true],
	[0, true, false, false],
	[0.1, true, false, true],
	[1, false, false, false],
	[1, true, true, false],
] as const) {
	test(`prompt cache distinguishes missing/zero writes: ${writes}, ${observed}, ${warm}`, async () => {
		const { input, config, runtime } = fixture();
		input.promptCache = {
			warm,
			cachingObserved: observed,
			cacheWriteTokens: writes,
			lastMissCauses: ["tools_changed", "model_changed"],
		};
		const segments = await buildSegments(input, config, runtime, false);
		expect(segments.includes("🧊 tools+")).toBe(visible);
	});
}

test("unknown cache causes remain useful and unknown attribution stays bare", async () => {
	const { input, config, runtime } = fixture();
	input.promptCache = {
		warm: false,
		cachingObserved: true,
		cacheWriteTokens: 1,
		lastMissCauses: ["future_changed"],
	};
	expect(await buildSegments(input, config, runtime, false)).toEqual([
		"🤖 Claude",
		"🧊 future",
	]);
	input.promptCache.lastMissCauses = ["unknown", "tools_changed"];
	expect(await buildSegments(input, config, runtime, false)).toEqual([
		"🤖 Claude",
		"🧊",
	]);
});

test("context, cache and compactions keep the segment order", async () => {
	const { input, config, runtime, files } = fixture();
	config.segments.compactions = true;
	input.contextPct = 67;
	input.promptCache = {
		warm: false,
		cachingObserved: true,
		cacheWriteTokens: 1,
		lastMissCauses: ["ttl_expired_1h"],
	};
	input.transcriptPath = "/session.jsonl";
	files.set(
		"/session.jsonl",
		encode('{"subtype":"compact_boundary"}\n{"subtype":"compact_boundary"}\n'),
	);
	expect(await buildSegments(input, config, runtime, false)).toEqual([
		"🤖 Claude",
		"\x1b[33m🧠 67%\x1b[0m",
		"🧊 ttl",
		"🔄 2",
	]);
});

test("repo and PR status icons stay outside their links", async () => {
	const { input, config, runtime } = fixture();
	config.segments.repo = true;
	input.workspace.repo = {
		host: "github.com",
		owner: "owner",
		name: "project",
	};
	input.pr = {
		number: 8,
		url: "https://github.com/owner/project/pull/8",
		reviewState: "draft",
		kind: "",
	};
	expect((await buildSegments(input, config, runtime, true)).at(-1)).toBe(
		"🐙 \x1b]8;;https://github.com/owner/project\x07owner/project\x1b]8;;\x07 📝 \x1b]8;;https://github.com/owner/project/pull/8\x07#8\x1b]8;;\x07",
	);
});

for (const [host, icon, prefix] of [
	["gitlab.com", "🦊", ""],
	["bitbucket.org", "🪣", ""],
	["code.example", "📦", "code.example/"],
	["", "📦", ""],
] as const) {
	test(`repository identity retains its host for ${host}`, async () => {
		const { input, config, runtime } = fixture();
		config.segments.repo = true;
		input.workspace.repo = { host, owner: "owner", name: "project" };
		expect((await buildSegments(input, config, runtime, false)).at(-1)).toBe(
			`${icon} ${prefix}owner/project`,
		);
	});
}

test("MR sigil and text-theme requested-changes color are preserved", async () => {
	const { input, config, runtime } = fixture();
	config.segments.repo = true;
	config.theme = "text";
	input.workspace.repo = {
		host: "gitlab.com",
		owner: "owner",
		name: "project",
	};
	input.pr = {
		number: 8,
		url: "",
		reviewState: "changes_requested",
		kind: "mr",
	};
	expect((await buildSegments(input, config, runtime, false)).at(-1)).toBe(
		"owner/project \x1b[31m!8\x1b[0m",
	);
});

test("workspace current_dir wins over cwd and worktree uses visible directory", async () => {
	const { input, config, runtime, files } = fixture();
	config.segments.repo = true;
	input.cwd = "/wrong";
	input.workspace.currentDir = "/workspace/side";
	input.workspace.repo = {
		host: "github.com",
		owner: "owner",
		name: "project",
	};
	files.set(
		"/workspace/side/.git",
		encode("gitdir: /main/.git/worktrees/side1\n"),
	);
	files.set(
		"/main/.git/worktrees/side1/HEAD",
		encode("ref: refs/heads/topic\n"),
	);
	expect((await buildSegments(input, config, runtime, false)).at(-1)).toBe(
		"🐙 owner/project 🌳 side 🌿 topic",
	);
	files.set(
		"/main/.git/worktrees/side1/HEAD",
		encode("ref: refs/heads/side\n"),
	);
	expect((await buildSegments(input, config, runtime, false)).at(-1)).toBe(
		"🐙 owner/project 🌳 side",
	);
});

test("bare worktree mode supports branch and stdin fallback without repo/PR", async () => {
	const { input, config, runtime, files } = fixture();
	config.segments.worktree = true;
	input.cwd = "/workspace";
	input.workspace.repo = {
		host: "github.com",
		owner: "owner",
		name: "project",
	};
	files.set("/workspace/.git", "directory");
	files.set("/workspace/.git/HEAD", encode("ref: refs/heads/topic\n"));
	expect((await buildSegments(input, config, runtime, false)).at(-1)).toBe(
		"🌿 topic",
	);
	files.delete("/workspace/.git/HEAD");
	input.workspace.gitWorktree = "fallback";
	expect((await buildSegments(input, config, runtime, false)).at(-1)).toBe(
		"🌿 fallback",
	);
});

test("secure quotas need no token and retain seven-day before five-hour order", async () => {
	const { input, config, runtime, calls } = fixture();
	config.segments.model = false;
	config.segments.quota = true;
	input.rateLimits.fiveHour = {
		usedPercentage: 25,
		resetsAtMs: nowMs + 300 * 60000,
	};
	input.rateLimits.sevenDay = {
		usedPercentage: 42,
		resetsAtMs: nowMs + 10080 * 60000,
	};
	expect(await buildSegments(input, config, runtime, false)).toEqual([
		"🔴 7d: 42% (7d 0h)",
		"🔴 5h: 25% (5h 0m)",
	]);
	expect(calls).toEqual([]);
});

test("unknown native reset is omitted instead of rendering epoch time", async () => {
	const { input, config, runtime } = fixture();
	config.segments.model = false;
	config.segments.quota = true;
	input.rateLimits.fiveHour = { usedPercentage: 25, resetsAtMs: undefined };
	expect(await buildSegments(input, config, runtime, false)).toEqual([]);
});

test("failed opt-in usage falls back to placeholder or stale account data", async () => {
	const { input, config, runtime, cache } = fixture();
	config.segments.model = false;
	config.segments.quota = true;
	config.macInsecure = true;
	expect(await buildSegments(input, config, runtime, false)).toEqual([
		"⏳ 7d: ?% (?d)",
		"⏳ 5h: ?% (?h)",
	]);
	cache.set(
		"usage-last-good",
		'{"five_hour":{"utilization":25,"resets_at":"2026-11-21T05:00:00Z"}}',
	);
	expect(await buildSegments(input, config, runtime, false)).toEqual([
		"🔴 5h: ?% (5h 0m)",
	]);
});

test("API credits use Go-compatible integer rounding and require opt-in", async () => {
	const { input, config, runtime, cache } = fixture();
	config.segments.model = false;
	config.segments.credits = true;
	cache.set(
		"usage",
		'{"extra_usage":{"is_enabled":true,"used_credits":2.5,"monthly_limit":10}}',
	);
	expect(await buildSegments(input, config, runtime, false)).toEqual([]);
	config.macInsecure = true;
	expect(await buildSegments(input, config, runtime, false)).toEqual([
		"💳 $2/$10",
	]);
});

test("per-model auto filters IDs before choosing a binding same-label window", async () => {
	const { input, config, runtime, cache } = fixture();
	config.segments.model = false;
	config.segments.quota = true;
	config.macInsecure = true;
	input.model = { id: "claude-opus-4-5", displayName: "Opus 4.5" };
	cache.set(
		"usage",
		JSON.stringify({
			limits: [
				{
					kind: "weekly_scoped",
					percent: 60,
					resets_at: "2026-11-28T00:00:00Z",
					scope: { model: { id: "claude-opus-4-5", display_name: "Opus" } },
				},
				{
					kind: "weekly_scoped",
					percent: 99,
					resets_at: "2026-11-28T00:00:00Z",
					scope: { model: { id: "claude-opus-4-6", display_name: "Opus" } },
				},
			],
		}),
	);
	expect(await buildSegments(input, config, runtime, false)).toEqual([
		"🔴 7d-opus: 60% (7d 0h)",
	]);
	config.segments.perModelQuota = "true";
	expect(await buildSegments(input, config, runtime, false)).toEqual([
		"🔴 7d-opus: 99% (7d 0h)",
	]);
	config.segments.perModelQuota = "false";
	expect(await buildSegments(input, config, runtime, false)).toEqual([]);
});

test("rate-limit state names only visible exhausted windows and retains stale quotas", async () => {
	const { input, config, runtime, cache } = fixture();
	config.segments.model = false;
	config.segments.quota = true;
	config.macInsecure = true;
	cache.set("usage", '{"error":{"type":"rate_limit_error"}}');
	cache.set(
		"usage-last-good",
		'{"five_hour":{"utilization":100,"resets_at":"2026-11-21T05:00:00Z"}}',
	);
	expect(await buildSegments(input, config, runtime, false)).toEqual([
		"⛔ 5h limit hit (5h 0m)",
		"🔴 5h: ?% (5h 0m)",
	]);
});

test("authentication response asks for login even with quotas hidden", async () => {
	const { input, config, runtime, cache } = fixture();
	config.segments.model = false;
	config.segments.credits = true;
	config.macInsecure = true;
	cache.set("usage", '{"error":{"type":"authentication_error"}}');
	expect(await buildSegments(input, config, runtime, false)).toEqual([
		"⚠️ /login needed",
	]);
});

test("all hidden segments yield empty output and wrapping remains explicit", async () => {
	const { input, config, runtime } = fixture();
	config.segments.model = false;
	expect(
		await renderStatusline(input, config, runtime, {
			maxWidth: 0,
			hyperlinks: false,
		}),
	).toBe("");
	config.segments.model = true;
	input.model.displayName = "Opus";
	input.contextPct = 37;
	expect(
		await renderStatusline(input, config, runtime, {
			maxWidth: 10,
			hyperlinks: false,
		}),
	).toBe("🤖 Opus\n\x1b[32m🧠 37%\x1b[0m");
});

test("compaction counting reuses the runtime cache between renders", async () => {
	const { input, config, runtime, files } = fixture();
	config.segments.compactions = true;
	input.transcriptPath = "/session.jsonl";
	files.set("/session.jsonl", encode('{"subtype":"compact_boundary"}\n'));
	const cache = emptyCompactionCache();
	runtime.compactionCache = cache;
	expect((await buildSegments(input, config, runtime, false)).at(-1)).toBe(
		"🔄 1",
	);
	expect(cache.offset).toBe(31);
	files.set(
		"/session.jsonl",
		encode('{"subtype":"compact_boundary"}\n{"subtype":"compact_boundary"}\n'),
	);
	expect((await buildSegments(input, config, runtime, false)).at(-1)).toBe(
		"🔄 2",
	);
	expect(cache.offset).toBe(62);
});
