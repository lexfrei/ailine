import { expect, test } from "bun:test";
import {
	defaults,
	loadConfigText,
	normalizeMode,
	normalizeTheme,
	parseDurationMs,
	validateConfigText,
} from "../../plugin/core/config";

const modes = [
	["", "auto"],
	["auto", "auto"],
	[" AUTO ", "auto"],
	["true", "true"],
	["True", "true"],
	["TRUE", "true"],
	["T", "true"],
	["1", "true"],
	["on", "true"],
	["false", "false"],
	["False", "false"],
	["FALSE", "false"],
	["F", "false"],
	["0", "false"],
	["off", "false"],
	[" yes ", undefined],
	["maybe", undefined],
	["2", undefined],
] as const;

for (const [raw, expected] of modes) {
	test(`normalizes mode ${JSON.stringify(raw)}`, () => {
		expect(normalizeMode(raw)).toBe(expected);
	});
}

for (const [raw, expected] of [
	["", "emoji"],
	["emoji", "emoji"],
	["text", "text"],
	["EMOJI", undefined],
	[" text ", undefined],
	["bogus", undefined],
] as const) {
	test(`normalizes theme ${JSON.stringify(raw)} without case folding`, () => {
		expect(normalizeTheme(raw)).toBe(expected);
	});
}

const durations = [
	["0", 0],
	["0s", 0],
	["1ns", 0.000001],
	["2us", 0.002],
	["2µs", 0.002],
	["2μs", 0.002],
	["15s", 15000],
	["120s", 120000],
	["10m", 600000],
	["2h45m", 9900000],
	[".5s", 500],
	["1.s", 1000],
	["1.25ms", 1.25],
	["0.0000000009s", 0],
	["+1m", 60000],
	["-5m", -300000],
	["", undefined],
	[" 1s", undefined],
	["1s ", undefined],
	["1", undefined],
	["1d", undefined],
	["1e3s", undefined],
	["NaN", undefined],
	["1s-2m", undefined],
	["9223372036854775808ns", undefined],
] as const;

for (const [raw, expected] of durations) {
	test(`parses Go duration ${JSON.stringify(raw)}`, () => {
		expect(parseDurationMs(raw)).toBe(expected);
	});
}

test("uses the Go whitespace set for mode values", () => {
	const nel = String.fromCodePoint(0x85);
	const bom = String.fromCodePoint(0xfeff);
	expect(normalizeMode(`${nel}true${nel}`)).toBe("true");
	expect(normalizeMode(`${bom}true`)).toBeUndefined();
});

test("returns independent enabled defaults", () => {
	const config = defaults();
	expect(config.theme).toBe("emoji");
	expect(config.hyperlinks).toBe("auto");
	expect(config.macInsecure).toBe(false);
	expect(config.cache).toEqual({ usageTtlMs: 600000, statusTtlMs: 15000 });
	expect(config.segments).toEqual({
		model: true,
		effort: true,
		thinking: true,
		fastMode: true,
		repo: true,
		worktree: true,
		cost: "auto",
		status: true,
		context: true,
		promptCache: true,
		compactions: true,
		quota: true,
		perModelQuota: "auto",
		credits: true,
	});
	config.segments.model = false;
	expect(defaults().segments.model).toBe(true);
});

test("missing and malformed configuration load silently uses defaults", () => {
	for (const source of [undefined, "", "not valid toml [[["]) {
		expect(loadConfigText(source)).toEqual({
			config: defaults(),
			warnings: [],
		});
	}
});

test("loads partial configuration with canonical mode and duration values", () => {
	const { config, warnings } = loadConfigText(
		'theme = "text"\nhyperlinks = false\n[segments]\ncost = false\nstatus = false\nper_model_quota = true\n[cache]\nusage_ttl = "120s"\nstatus_ttl = "30s"\n',
	);
	expect(warnings).toEqual([]);
	expect(config.theme).toBe("text");
	expect(config.hyperlinks).toBe("false");
	expect(config.segments.cost).toBe("false");
	expect(config.segments.status).toBe(false);
	expect(config.segments.perModelQuota).toBe("true");
	expect(config.segments.model).toBe(true);
	expect(config.cache).toEqual({ usageTtlMs: 120000, statusTtlMs: 30000 });
});

test("normalizes configuration keys case-insensitively", () => {
	const { config, warnings } = loadConfigText(
		'Theme = "text"\n[Segments]\nMODEL = false\nFast_Mode = false\n',
	);
	expect(warnings).toEqual([]);
	expect(config.theme).toBe("text");
	expect(config.segments.model).toBe(false);
	expect(config.segments.fastMode).toBe(false);
});

test("preserves weak scalar loading but validates raw boolean types", () => {
	const source =
		'mac_insecure = "true"\n[segments]\nmodel = "false"\ncontext = 0\n';
	const { config, warnings } = loadConfigText(source);
	expect(warnings).toEqual([]);
	expect(config.macInsecure).toBe(true);
	expect(config.segments.model).toBe(false);
	expect(config.segments.context).toBe(false);
	expect(validateConfigText(source)).toEqual(
		expect.arrayContaining([
			"segments.model: expected true or false, got false",
			"segments.context: expected true or false, got 0",
			"mac_insecure: expected true or false, got true",
		]),
	);
});

test("numeric cache scalars use Go nanoseconds, not milliseconds", () => {
	expect(
		loadConfigText("[cache]\nusage_ttl = 15000000000\n").config.cache
			.usageTtlMs,
	).toBe(15000);
});

test("preserves boolean-to-nanosecond duration coercion", () => {
	const source = "[cache]\nusage_ttl = false\nstatus_ttl = true\n";
	expect(loadConfigText(source)).toEqual({
		config: { ...defaults(), cache: { usageTtlMs: 0, statusTtlMs: 0.000001 } },
		warnings: [],
	});
	expect(validateConfigText(source)).toEqual([]);
});

test("an unmarshal failure discards the entire configuration", () => {
	const { config, warnings } = loadConfigText(
		'[segments]\nmodel = false\ncredits = "not-a-bool"\n',
	);
	expect(config).toEqual(defaults());
	expect(warnings).toHaveLength(1);
	expect(warnings[0]).toContain("ailine: config parse error:");
});

test("invalid modes warn and recover without discarding other fields", () => {
	const { config, warnings } = loadConfigText(
		'theme = "bogus"\nhyperlinks = "bogus"\n[segments]\nmodel = false\ncost = "bogus"\nper_model_quota = "bogus"\n',
	);
	expect(config.theme).toBe("emoji");
	expect(config.hyperlinks).toBe("auto");
	expect(config.segments.model).toBe(false);
	expect(config.segments.cost).toBe("auto");
	expect(config.segments.perModelQuota).toBe("auto");
	expect(warnings).toEqual(
		expect.arrayContaining([
			'ailine: invalid cost mode "bogus", using auto',
			'ailine: invalid per-model quota mode "bogus", using auto',
			'ailine: invalid theme "bogus", using emoji',
			'ailine: invalid hyperlinks mode "bogus", using auto',
		]),
	);
});

test("loading tolerates unknown keys while validate reports them", () => {
	const source = "[segments]\nmodel = false\nmodle = true\n";
	expect(loadConfigText(source).config.segments.model).toBe(false);
	expect(validateConfigText(source)).toEqual([
		'unknown key "segments.modle" (typo?)',
	]);
});

test("deprecated offpeak remains an ignored accepted key", () => {
	expect(validateConfigText("[segments]\noffpeak = false\n")).toEqual([]);
});

test("validate reports invalid modes with the original scalar", () => {
	const problems = validateConfigText(
		'theme = "bogus"\nhyperlinks = "broken"\n[segments]\ncost = "yes"\nper_model_quota = "wat"\n',
	);
	expect(problems).toEqual(
		expect.arrayContaining([
			'segments.cost: unknown value "yes" (expected auto, true, or false)',
			'segments.per_model_quota: unknown value "wat" (expected auto, true, or false)',
			'theme: unknown value "bogus" (expected emoji or text)',
			'hyperlinks: unknown value "broken" (expected auto, true, or false)',
		]),
	);
});

test("validate rejects negative cache durations in Go display form", () => {
	expect(
		validateConfigText('[cache]\nusage_ttl = "-5m"\nstatus_ttl = "-1h"\n'),
	).toEqual([
		"cache.usage_ttl: negative duration -5m0s",
		"cache.status_ttl: negative duration -1h0m0s",
	]);
});

test("validate rejects malformed TOML instead of silently accepting defaults", () => {
	const problems = validateConfigText("theme = [");
	expect(problems).toHaveLength(1);
	expect(problems[0]).toContain("cannot read config:");
	expect(problems[0]).not.toContain("\n");
	expect(problems[0]).toBe(
		"cannot read config: Invalid TOML document: unfinished array (line 1, column 9)",
	);
	expect(validateConfigText("a = 1\nb = [")[0]).toContain("(line 2, column 5)");
});

test("unsafe TOML keys cannot modify default objects", () => {
	loadConfigText("[__proto__]\nailineConfigPolluted = true\n");
	expect(({} as Record<string, unknown>).ailineConfigPolluted).toBeUndefined();
	expect(defaults().segments.model).toBe(true);
});
