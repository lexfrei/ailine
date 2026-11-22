import { expect, test } from "bun:test";
import { emptyInput, parseInput } from "../../plugin/core/input";

function parse(body: unknown) {
	return parseInput(JSON.stringify(body));
}

test("full stdin fixture normalizes every supported field", () => {
	const result = parse({
		model: { id: "claude-opus-4-8", display_name: "Opus 4.8" },
		effort: { level: "high" },
		thinking: { enabled: true },
		fast_mode: true,
		cwd: "/work",
		workspace: {
			current_dir: "/work/tree",
			git_worktree: "feature",
			repo: { host: "github.com", owner: "example", name: "project" },
		},
		pr: {
			number: 4294967297,
			url: "https://example.com/pr/1",
			review_state: "approved",
			kind: "mr",
		},
		cost: { total_cost_usd: 1.23 },
		context_window: { used_percentage: 30.5 },
		prompt_cache: {
			warm: false,
			caching_observed: true,
			cache_write_tokens: 4200.5,
			last_miss_cause: { causes: ["tools_changed", "system_prompt_changed"] },
		},
		transcript_path: "/work/transcript.jsonl",
		rate_limits: {
			five_hour: { used_percentage: 30, resets_at: 1700000000.9 },
			seven_day: { used_percentage: 55, resets_at: 9999999999 },
		},
	});
	expect(result).toEqual({
		input: {
			model: { id: "claude-opus-4-8", displayName: "Opus 4.8" },
			effortLevel: "high",
			thinking: true,
			fastMode: true,
			cwd: "/work",
			workspace: {
				currentDir: "/work/tree",
				gitWorktree: "feature",
				repo: { host: "github.com", owner: "example", name: "project" },
			},
			pr: {
				number: 4294967297,
				url: "https://example.com/pr/1",
				reviewState: "approved",
				kind: "mr",
			},
			costUsd: 1.23,
			contextPct: 30.5,
			promptCache: {
				warm: false,
				cachingObserved: true,
				cacheWriteTokens: 4200.5,
				lastMissCauses: ["tools_changed", "system_prompt_changed"],
			},
			transcriptPath: "/work/transcript.jsonl",
			rateLimits: {
				fiveHour: { usedPercentage: 30, resetsAtMs: 1700000000000 },
				sevenDay: { usedPercentage: 55, resetsAtMs: 9999999999000 },
			},
		},
		warnings: [],
	});
});

test("empty input and nullable absent values preserve independent zero defaults", () => {
	const expected = {
		model: { id: "", displayName: "" },
		effortLevel: "",
		thinking: false,
		fastMode: false,
		cwd: "",
		workspace: { currentDir: "", gitWorktree: "", repo: undefined },
		pr: undefined,
		costUsd: 0,
		contextPct: 0,
		promptCache: undefined,
		transcriptPath: "",
		rateLimits: { fiveHour: undefined, sevenDay: undefined },
	};
	expect(emptyInput()).toEqual(expected);
	for (const raw of [
		"",
		"{}",
		"null",
		'{"model":null,"effort":null,"thinking":null,"fast_mode":null,"cwd":null,"workspace":null,"pr":null,"cost":null,"context_window":null,"prompt_cache":null,"transcript_path":null,"rate_limits":null}',
	])
		expect(parseInput(raw)).toEqual({ input: expected, warnings: [] });
	const mutated = emptyInput();
	mutated.model.id = "changed";
	mutated.workspace.currentDir = "changed";
	mutated.rateLimits.fiveHour = { usedPercentage: 1, resetsAtMs: undefined };
	expect(emptyInput()).toEqual(expected);
	expect(parse({}).input).toEqual(expected);
});

test("malformed nonempty input returns one sanitized warning without partial data", () => {
	for (const raw of [
		"not json",
		" \n\t",
		"﻿{}",
		'{"cwd":"secret-token",}',
		'{"cwd":"valid"} trailing',
	]) {
		const result = parseInput(raw);
		expect(result.input).toEqual(emptyInput());
		expect(result.warnings).toEqual([
			"ailine: stdin parse error: invalid input",
		]);
	}
});

test("wrong root values warn and retain defaults", () => {
	for (const body of [true, 1, "text", []]) {
		const result = parse(body);
		expect(result.input).toEqual(emptyInput());
		expect(result.warnings).toHaveLength(1);
	}
});

test("scalar type errors preserve valid siblings and report only one warning", () => {
	const result = parse({
		model: { id: 123, display_name: "Opus" },
		effort: { level: true },
		thinking: { enabled: "true" },
		fast_mode: 1,
		cwd: "/valid",
		workspace: {
			current_dir: false,
			git_worktree: "branch",
			repo: { host: [], owner: "owner", name: "repo" },
		},
		pr: {
			number: 1.5,
			url: "https://example.com",
			review_state: 1,
			kind: "mr",
		},
		cost: { total_cost_usd: "1" },
		context_window: { used_percentage: 42 },
		prompt_cache: {
			warm: "false",
			caching_observed: true,
			cache_write_tokens: "3",
			last_miss_cause: { causes: ["tools_changed", 3, null, "ttl_expired_1h"] },
		},
		transcript_path: {},
		rate_limits: {
			five_hour: { used_percentage: "30", resets_at: 12.9 },
			seven_day: { used_percentage: 55, resets_at: "12" },
		},
	});
	expect(result.warnings).toHaveLength(1);
	expect(result.input).toEqual({
		model: { id: "", displayName: "Opus" },
		effortLevel: "",
		thinking: false,
		fastMode: false,
		cwd: "/valid",
		workspace: {
			currentDir: "",
			gitWorktree: "branch",
			repo: { host: "", owner: "owner", name: "repo" },
		},
		pr: { number: 0, url: "https://example.com", reviewState: "", kind: "mr" },
		costUsd: 0,
		contextPct: 42,
		promptCache: {
			warm: false,
			cachingObserved: true,
			cacheWriteTokens: 0,
			lastMissCauses: ["tools_changed", "", "", "ttl_expired_1h"],
		},
		transcriptPath: "",
		rateLimits: {
			fiveHour: { usedPercentage: 0, resetsAtMs: 12000 },
			sevenDay: { usedPercentage: 55, resetsAtMs: 0 },
		},
	});
});

test("provided pointer objects stay present even when empty or wrong typed", () => {
	for (const value of [{}, true, [], "bad", 1]) {
		const result = parse({
			workspace: { repo: value },
			pr: value,
			prompt_cache: value,
			rate_limits: { five_hour: value },
		});
		expect(result.input.workspace.repo).toEqual({
			host: "",
			owner: "",
			name: "",
		});
		expect(result.input.pr).toEqual({
			number: 0,
			url: "",
			reviewState: "",
			kind: "",
		});
		expect(result.input.promptCache).toEqual({
			warm: false,
			cachingObserved: false,
			cacheWriteTokens: undefined,
			lastMissCauses: undefined,
		});
		expect(result.input.rateLimits.fiveHour).toEqual({
			usedPercentage: 0,
			resetsAtMs: 0,
		});
		expect(result.warnings).toHaveLength(
			typeof value === "object" && !Array.isArray(value) ? 0 : 1,
		);
	}
});

test("nonpointer objects reject nonobjects without losing independent fields", () => {
	for (const key of [
		"model",
		"effort",
		"thinking",
		"workspace",
		"cost",
		"context_window",
		"rate_limits",
	]) {
		const result = parse({ [key]: [], transcript_path: "/valid" });
		expect(result.input).toEqual({ ...emptyInput(), transcriptPath: "/valid" });
		expect(result.warnings).toHaveLength(1);
	}
});

test("nullable pointer values differ from explicit zero and fractional metered writes", () => {
	for (const value of [undefined, null, 0, 0.25, -1]) {
		const result = parse({
			prompt_cache: { cache_write_tokens: value },
			rate_limits: { five_hour: null, seven_day: {} },
		});
		expect(result.input.promptCache?.cacheWriteTokens).toBe(value ?? undefined);
		expect(result.input.rateLimits.fiveHour).toBeUndefined();
		expect(result.input.rateLimits.sevenDay).toEqual({
			usedPercentage: 0,
			resetsAtMs: 0,
		});
		expect(result.warnings).toEqual([]);
	}
});

test("miss cause slices preserve nil versus empty and null string elements", () => {
	for (const value of [undefined, null, {}, { causes: null }])
		expect(
			parse({ prompt_cache: { last_miss_cause: value } }).input.promptCache
				?.lastMissCauses,
		).toBeUndefined();
	for (const causes of [[], [null], ["tools_changed", null]]) {
		const result = parse({ prompt_cache: { last_miss_cause: { causes } } });
		expect(result.input.promptCache?.lastMissCauses).toEqual(
			causes.map((cause) => cause ?? ""),
		);
		expect(result.warnings).toEqual([]);
	}
	for (const value of [true, { causes: {} }, { causes: "tools_changed" }]) {
		const result = parse({
			prompt_cache: { warm: true, last_miss_cause: value },
		});
		expect(result.input.promptCache?.warm).toBe(true);
		expect(result.input.promptCache?.lastMissCauses).toBeUndefined();
		expect(result.warnings).toHaveLength(1);
	}
});

test("reset seconds truncate toward zero and null reset means known zero", () => {
	for (const [reset, expected] of [
		[12.9, 12000],
		[-12.9, -12000],
		[-0.5, 0],
		[0, 0],
		[null, 0],
		[undefined, 0],
	] as const) {
		const result = parse({
			rate_limits: { five_hour: { used_percentage: -20.5, resets_at: reset } },
		});
		expect(result.input.rateLimits.fiveHour).toEqual({
			usedPercentage: -20.5,
			resetsAtMs: expected,
		});
		expect(result.warnings).toEqual([]);
	}
});

test("out of domain reset seconds warn and fall back to finite zero milliseconds", () => {
	for (const reset of [1e308, -1e308, 2 ** 63, Number.MAX_SAFE_INTEGER]) {
		const result = parse({
			rate_limits: { five_hour: { used_percentage: 30, resets_at: reset } },
		});
		expect(result.input.rateLimits.fiveHour).toEqual({
			usedPercentage: 30,
			resetsAtMs: 0,
		});
		expect(result.warnings).toHaveLength(1);
	}
});

test("known numeric overflow zeroes only the offending field", () => {
	for (const raw of [
		'{"cost":{"total_cost_usd":1e999},"cwd":"ok"}',
		'{"context_window":{"used_percentage":-1e999},"cwd":"ok"}',
		'{"pr":{"number":1e999},"cwd":"ok"}',
		'{"prompt_cache":{"cache_write_tokens":1e999},"cwd":"ok"}',
		'{"rate_limits":{"five_hour":{"used_percentage":1e999,"resets_at":1e999}},"cwd":"ok"}',
	]) {
		const result = parseInput(raw);
		expect(result.input.cwd).toBe("ok");
		expect(result.input.costUsd).toBe(0);
		expect(result.input.contextPct).toBe(0);
		expect(result.input.pr?.number ?? 0).toBe(0);
		expect(result.input.promptCache?.cacheWriteTokens ?? 0).toBe(0);
		expect(result.input.rateLimits.fiveHour?.resetsAtMs ?? 0).toBe(0);
		expect(result.warnings).toHaveLength(1);
	}
});

test("PR numbers retain safe 64-bit range without 32-bit truncation", () => {
	for (const number of [
		0,
		-1,
		2147483648,
		4294967297,
		Number.MAX_SAFE_INTEGER,
	]) {
		const result = parse({ pr: { number } });
		expect(result.input.pr?.number).toBe(number);
		expect(result.warnings).toEqual([]);
	}
	for (const number of [
		1.5,
		Number.MAX_SAFE_INTEGER + 1,
		2 ** 63,
		-(2 ** 63) - 2048,
	]) {
		const result = parse({ pr: { number } });
		expect(result.input.pr?.number).toBe(0);
		expect(result.warnings).toHaveLength(1);
	}
});

test("all selected strings replace unpaired surrogates without trimming", () => {
	const value = " 𐏿\ud800 \udfff ";
	const expected = value.toWellFormed();
	const input = parse({
		model: { id: value, display_name: value },
		effort: { level: value },
		cwd: value,
		workspace: {
			current_dir: value,
			git_worktree: value,
			repo: { host: value, owner: value, name: value },
		},
		pr: { url: value, review_state: value, kind: value },
		transcript_path: value,
		prompt_cache: { last_miss_cause: { causes: [value] } },
	}).input;
	expect(input.model).toEqual({ id: expected, displayName: expected });
	expect(input.effortLevel).toBe(expected);
	expect(input.cwd).toBe(expected);
	expect(input.workspace).toEqual({
		currentDir: expected,
		gitWorktree: expected,
		repo: { host: expected, owner: expected, name: expected },
	});
	expect(input.pr).toEqual({
		number: 0,
		url: expected,
		reviewState: expected,
		kind: expected,
	});
	expect(input.transcriptPath).toBe(expected);
	expect(input.promptCache?.lastMissCauses).toEqual([expected]);
});

test("future unknown fields and numeric overflows are ignored within Go nesting limit", () => {
	expect(
		parseInput(
			'{"future":1e999,"model":{"future":true,"display_name":"Opus"},"prompt_cache":{"hit_ratio":{},"future":[]}}',
		).warnings,
	).toEqual([]);
	const unknown = `"future":${"[".repeat(9999)}1e999${"]".repeat(9999)}`;
	expect(parseInput(`{${unknown}}`).warnings).toEqual([]);
	expect(parseInput(`{"too_deep":{${unknown}}}`).warnings).toHaveLength(1);
});
