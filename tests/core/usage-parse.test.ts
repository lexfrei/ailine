import { expect, test } from "bun:test";
import { parseUsageBody } from "../../plugin/core/usage-parse";

const nowMs = Date.UTC(2099, 0, 1);
const resetsAt = "2099-01-01T03:00:00Z";
const window = { utilization: 30.5, resets_at: resetsAt };

function parse(body: unknown) {
	const result = parseUsageBody(JSON.stringify(body), nowMs);
	if (result.error !== undefined) throw result.error;
	return result;
}

test("legacy windows retain utilization and use caller time", () => {
	const data = parse({
		five_hour: window,
		seven_day: { ...window, utilization: 45.2 },
		seven_day_opus: { ...window, utilization: 12.5 },
		seven_day_sonnet: { ...window, utilization: 78.9 },
		seven_day_cowork: null,
		seven_day_oauth_apps: { ...window, utilization: 5 },
	}).data;
	expect(data.fiveHour).toEqual({
		utilization: 30.5,
		resetsAtMs: nowMs + 10800000,
		totalMinutes: 300,
		remainingMinutes: 180,
	});
	for (const [key, utilization] of [
		["sevenDay", 45.2],
		["sevenDayOpus", 12.5],
		["sevenDaySonnet", 78.9],
		["sevenDayOAuthApps", 5],
	] as const)
		expect(data[key]).toEqual({
			utilization,
			resetsAtMs: nowMs + 10800000,
			totalMinutes: 10080,
			remainingMinutes: 180,
		});
	expect(data.sevenDayCowork).toBeUndefined();
	expect(data.scoped).toBeUndefined();
	expect(data.extra).toBeUndefined();
});

test("window reset validation truncation and unclamped utilization", () => {
	for (const reset of ["invalid", "", null])
		expect(
			parse({ five_hour: { resets_at: reset } }).data.fiveHour,
		).toBeUndefined();
	for (const [offset, remaining] of [
		[-60001, 0],
		[-1, 0],
		[59999, 0],
		[60001, 1],
	] as const) {
		const result = parseUsageBody(
			JSON.stringify({ five_hour: { utilization: -20, resets_at: resetsAt } }),
			nowMs + 10800000 - offset,
		);
		expect(result.data?.fiveHour?.remainingMinutes).toBe(remaining);
		expect(result.data?.fiveHour?.utilization).toBe(-20);
	}
});

test("empty and nullable structs preserve zero defaults", () => {
	for (const body of [
		null,
		{},
		{ limits: null },
		{ limits: [] },
		{ limits: [null, {}] },
		{ error: null, five_hour: null, extra_usage: null },
	]) {
		const result = parse(body);
		expect(result.cacheable).toBe(true);
		expect(result.data.fiveHour).toBeUndefined();
		expect(result.data.scoped).toBeUndefined();
	}
	expect(
		parse({ five_hour: { utilization: null, resets_at: resetsAt } }).data
			.fiveHour?.utilization,
	).toBe(0);
});

test("API errors short circuit output but not field validation", () => {
	for (const type of [
		"authentication_error",
		"rate_limit_error",
		"anything",
		"",
		null,
	]) {
		const result = parse({ error: { type }, five_hour: window });
		expect(result.data).toEqual({ errorType: type ?? "" });
		expect(result.cacheable).toBe(false);
	}
	expect(parse({ error: {} }).data).toEqual({ errorType: "" });
	expect(parse({ error: {} }).cacheable).toBe(false);
	expect(
		parseUsageBody('{"error":{},"five_hour":true}', nowMs).error,
	).toBeInstanceOf(Error);
});

test("extra usage keeps raw API numbers and requires enabled positive usage", () => {
	expect(
		parse({
			extra_usage: { is_enabled: true, monthly_limit: 5000, used_credits: 128 },
		}).data.extra,
	).toEqual({ monthlyLimit: 5000, usedCredits: 128 });
	for (const extra of [
		{},
		{ is_enabled: false, used_credits: 128 },
		{ is_enabled: true, used_credits: 0 },
		{ is_enabled: true, used_credits: -1 },
		{ is_enabled: null, used_credits: null, monthly_limit: null },
	])
		expect(parse({ extra_usage: extra }).data.extra).toBeUndefined();
	expect(
		parse({ extra_usage: { is_enabled: true, used_credits: 1 } }).data.extra,
	).toEqual({ monthlyLimit: 0, usedCredits: 1 });
});

test("scoped weekly windows keep inactive buckets order and trimmed model identity", () => {
	const limits = [
		{ kind: "session", percent: 58, resets_at: resetsAt, scope: null },
		{ kind: "weekly_all", percent: 78, resets_at: resetsAt, scope: null },
		{
			kind: "weekly_scoped",
			percent: 100,
			resets_at: resetsAt,
			is_active: true,
			severity: "critical",
			scope: { model: { id: null, display_name: "Fable" } },
		},
		{
			kind: "weekly_scoped",
			percent: 120,
			resets_at: resetsAt,
			is_active: false,
			severity: {},
			scope: {
				model: {
					id: "\u0085model-id\u0085",
					display_name: "\u0085 Sonnet \u0085",
				},
			},
		},
	];
	const scoped = parse({ limits }).data.scoped;
	expect(scoped?.map(({ name, id }) => ({ name, id }))).toEqual([
		{ name: "Fable", id: "" },
		{ name: "Sonnet", id: "model-id" },
	]);
	expect(scoped?.[1]?.window).toEqual({
		utilization: 120,
		resetsAtMs: nowMs + 10800000,
		totalMinutes: 10080,
		remainingMinutes: 180,
	});
});

test("scoped parsing skips nonmodel blank and invalid reset entries", () => {
	const entries = [
		{},
		{ scope: null },
		{ scope: { model: null, surface: { display_name: "Cowork" } } },
		{ scope: { model: { display_name: "   " } } },
		{ scope: { model: { display_name: null } } },
		{ resets_at: "invalid", scope: { model: { display_name: "Broken" } } },
	];
	expect(
		parse({
			limits: entries.map((entry) => ({
				kind: "weekly_scoped",
				resets_at: resetsAt,
				...entry,
			})),
		}).data.scoped,
	).toBeUndefined();
});

test("known scalar and object type mismatches fail even when unused", () => {
	const invalid = [
		true,
		1,
		"text",
		[],
		{ five_hour: [] },
		{ seven_day: false },
		{ seven_day_opus: "x" },
		{ seven_day_sonnet: 1 },
		{ seven_day_cowork: [] },
		{ seven_day_oauth_apps: true },
		{ five_hour: { utilization: "30" } },
		{ five_hour: { resets_at: 1 } },
		{ error: true },
		{ error: { type: 1 } },
		{ extra_usage: [] },
		{ extra_usage: { is_enabled: "true" } },
		{ extra_usage: { is_enabled: false, monthly_limit: "5" } },
		{ extra_usage: { used_credits: true } },
		{ limits: {} },
		{ limits: [1] },
		{ limits: [[]] },
		{ limits: [{ kind: 1 }] },
		{ limits: [{ percent: "1" }] },
		{ limits: [{ resets_at: false }] },
		{ limits: [{ scope: [] }] },
		{ limits: [{ scope: { model: false } }] },
		{ limits: [{ scope: { model: { id: 1 } } }] },
		{ limits: [{ scope: { model: { display_name: [] } } }] },
	];
	for (const body of invalid) {
		const result = parseUsageBody(JSON.stringify(body), nowMs);
		expect(result.data).toBeUndefined();
		expect(result.error).toBeInstanceOf(Error);
		expect(result.cacheable).toBe(false);
	}
});

test("malformed payload and known numeric overflow fail without leaking content", () => {
	for (const raw of [
		"not json",
		'{"secret-token":}',
		'{"five_hour":{"utilization":1e999}}',
		'{"limits":[{"percent":-1e999}]}',
		'{"extra_usage":{"monthly_limit":1e999}}',
		'{"extra_usage":{"used_credits":1e999}}',
	]) {
		const result = parseUsageBody(raw, nowMs);
		expect(result.error).toBeInstanceOf(Error);
		expect(result.data).toBeUndefined();
		expect(result.cacheable).toBe(false);
		expect(result.error?.message).not.toContain("secret-token");
		expect(result.error?.cause).toBeUndefined();
	}
});

test("selected strings replace escaped lone surrogates and ignore unknown subtrees", () => {
	expect(
		parseUsageBody('{"error":{"type":"\\ud800"}}', nowMs).data?.errorType,
	).toBe("�");
	const result = parseUsageBody(
		'{"limits":[{"kind":"weekly_scoped","resets_at":"2099-01-01T03:00:00Z","scope":{"model":{"display_name":"\\ud800","id":"\\udfff"}}}]}',
		nowMs,
	);
	expect(result.data?.scoped?.[0]?.name).toBe("�");
	expect(result.data?.scoped?.[0]?.id).toBe("�");
	expect(
		parseUsageBody(
			`{"unknown":${"[".repeat(9999)}1e999${"]".repeat(9999)}}`,
			nowMs,
		).cacheable,
	).toBe(true);
});
