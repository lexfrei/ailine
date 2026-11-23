import { expect, test } from "bun:test";
import {
	contextSegment,
	duration,
	findExhaustedWindow,
	fiveHourWindowMinutes,
	formatQuotaWindow,
	formatRateLimitSegment,
	formatStaleQuotaWindow,
	parseIsoUtc,
	part,
	promptCacheSegment,
	rateIndicator,
	sevenDayWindowMinutes,
} from "../../plugin/core/format";
import type { QuotaWindow, Theme } from "../../plugin/core/types";

const win = (
	utilization: number,
	remainingMinutes = 150,
	totalMinutes = 300,
): QuotaWindow => ({
	utilization,
	remainingMinutes,
	totalMinutes,
	resetsAtMs: 0,
});

test("unknown icons cannot resolve inherited object properties", () => {
	expect(part("text", "plain", "constructor")).toBe("plain");
	expect(part("text", "plain", "toString")).toBe("plain");
});

test("duration Go table", () => {
	for (const [minutes, want] of [
		[0, "now"],
		[-5, "now"],
		[7, "7m"],
		[60, "1h 0m"],
		[125, "2h 5m"],
		[1440, "1d 0h"],
		[1500, "1d 1h"],
		[10080, "7d 0h"],
		[4337, "3d 0h"],
	] as const)
		expect(duration(minutes)).toBe(want);
	expect(fiveHourWindowMinutes).toBe(300);
	expect(sevenDayWindowMinutes).toBe(10080);
});
test("rate thresholds and integer elapsed percentages", () => {
	for (const [usage, remaining, total, want] of [
		[45, 150, 300, "🟢"],
		[53, 150, 300, "🟡"],
		[60, 150, 300, "🟠"],
		[70, 150, 300, "🔴"],
		[0, 300, 300, "🟢"],
		[100, 0, 300, "🟢"],
		[50, 150, 300, "🟢"],
		[55, 150, 300, "🟡"],
		[65, 150, 300, "🟠"],
		[66, 150, 300, "🔴"],
		[1, 2, 3, "🟢"],
		[34, 2, 3, "🟡"],
		[0, 400, 300, "🟢"],
		[6, 1, 0, "🟠"],
	] as const)
		expect(rateIndicator(usage, remaining, total)).toBe(want);
});
test("part icon placement and text-theme status colors", () => {
	for (const [text, icons, want] of [
		["o/r", ["🐙"], "🐙 o/r"],
		["#19", ["📝"], "📝 #19"],
		["quota", ["🟡", "⬆"], "🟡 quota ⬆"],
		["Opus", ["🤖", "⏫", "💭", "⚡"], "🤖 Opus ⏫💭⚡"],
		["Opus", ["🤖", "", ""], "🤖 Opus"],
		["#19", [], "#19"],
		["2", ["🔄"], "🔄 2"],
		["x", ["", "⚡"], "x ⚡"],
		["", ["🤖"], "🤖 "],
	] as [string, string[], string][])
		expect(part("emoji", text, ...icons)).toBe(want);
	for (const [icon, color] of [
		["🟢", "32"],
		["🟡", "33"],
		["🟠", "38;5;208"],
		["🔴", "31"],
		["🔶", "38;5;208"],
		["⚠️", "33"],
	])
		expect(part("text", "x", icon ?? "")).toBe(`\x1b[${color}mx\x1b[0m`);
	expect(part("text", "x", "🤖", "⏫", "💭")).toBe("x");
	expect(part("text", "x", "🟢", "🔴")).toBe("\x1b[32mx\x1b[0m");
});
test("context thresholds round by truncating plus half, both themes", () => {
	for (const theme of ["emoji", "text"] as Theme[])
		for (const [pct, rounded, color] of [
			[30, 30, 32],
			[49.4, 49, 32],
			[49.5, 50, 33],
			[50, 50, 33],
			[79, 79, 33],
			[79.5, 80, 31],
			[80, 80, 31],
			[95, 95, 31],
			[-1.6, -1, 32],
		])
			expect(contextSegment(theme, pct ?? 0)).toBe(
				`\x1b[${color}m${theme === "emoji" ? "🧠 " : ""}${rounded}%\x1b[0m`,
			);
});
test("cache and quota formatting retain exact bytes", () => {
	expect(promptCacheSegment("emoji", "tools")).toBe("🧊 tools");
	expect(promptCacheSegment("emoji", "")).toBe("🧊");
	expect(promptCacheSegment("text", "tools")).toBe("cache: tools");
	expect(promptCacheSegment("text", "")).toBe("cache: cold");
	expect(formatQuotaWindow("emoji", win(45.3, 6857, 10080), "7d")).toBe(
		"🟠 7d: 45% (4d 18h)",
	);
	expect(formatStaleQuotaWindow("emoji", win(45.3, 6857, 10080), "7d")).toBe(
		"🟠 7d: ?% (4d 18h)",
	);
	expect(formatQuotaWindow("text", win(10, 9000, 10080), "7d")).toBe(
		"\x1b[32m7d: 10% (6d 6h)\x1b[0m",
	);
	expect(formatStaleQuotaWindow("text", win(55), "5h")).toBe(
		"\x1b[33m5h: ?% (2h 30m)\x1b[0m",
	);
	for (const [input, want] of [
		[undefined, "limit hit"],
		[{ name: "5h", minutes: 134 }, "5h limit hit (2h 14m)"],
		[{ name: "7d", minutes: 1440 }, "7d limit hit (1d 0h)"],
		[{ name: "5h", minutes: 0 }, "5h limit hit"],
		[{ name: "5h", minutes: -2 }, "5h limit hit"],
	] as const) {
		expect(formatRateLimitSegment("emoji", input)).toBe(`⛔ ${want}`);
		expect(formatRateLimitSegment("text", input)).toBe(want);
	}
});
test("exhausted selection active, rounded threshold, saturation then earliest reset", () => {
	expect(findExhaustedWindow(undefined, [])).toBeUndefined();
	expect(findExhaustedWindow({}, [])).toBeUndefined();
	expect(
		findExhaustedWindow({ fiveHour: win(50), sevenDay: win(60) }, []),
	).toBeUndefined();
	expect(
		findExhaustedWindow({ fiveHour: win(88, 45), sevenDay: win(99, 125) }, []),
	).toEqual({ name: "7d", minutes: 125 });
	expect(
		findExhaustedWindow({ fiveHour: win(100, 30), sevenDay: win(50) }, []),
	).toEqual({ name: "5h", minutes: 30 });
	expect(
		findExhaustedWindow({ fiveHour: win(100, 0), sevenDay: win(100, -1) }, []),
	).toBeUndefined();
	expect(findExhaustedWindow({ fiveHour: win(98.49) }, [])).toBeUndefined();
	expect(findExhaustedWindow({ fiveHour: win(98.5) }, [])).toEqual({
		name: "5h",
		minutes: 150,
	});
	expect(
		findExhaustedWindow({ fiveHour: win(99, 20), sevenDay: win(100, 60) }, []),
	).toEqual({ name: "7d", minutes: 60 });
	expect(
		findExhaustedWindow({ fiveHour: win(100, 60), sevenDay: win(100, 20) }, []),
	).toEqual({ name: "7d", minutes: 20 });
	expect(
		findExhaustedWindow({ fiveHour: win(100, 20), sevenDay: win(100, 20) }, []),
	).toEqual({ name: "5h", minutes: 20 });
	const scoped = [
		{ name: "Fable", id: "", window: win(100, 10) },
		{ name: "empty", id: "", window: undefined },
	];
	expect(findExhaustedWindow({}, scoped)).toEqual({
		name: "7d-fable",
		minutes: 10,
	});
	expect(findExhaustedWindow({ sevenDayOpus: win(100) }, [])).toBeUndefined();
});
test("ISO UTC accepts timestamps only and rejects normalized invalid calendars", () => {
	for (const raw of [
		"2025-02-26T12:34:56Z",
		"2025-02-26T12:34:56+00:00",
		" 2025-02-26T12:34:56Z ",
	])
		expect(parseIsoUtc(raw)).toBe(1740573296000);
	for (const raw of [
		"2025-02-26T12:34:56.123456Z",
		"2025-02-26T12:34:56.123456+00:00",
		"2025-02-26T15:34:56.123+03:00",
	])
		expect(parseIsoUtc(raw)).toBe(1740573296123);
	expect(parseIsoUtc("2024-02-29T00:00:00Z")).toBe(1709164800000);
	expect(parseIsoUtc("0000-01-01T00:00:00Z")).toBe(-62167219200000);
	for (const raw of [
		"",
		"not-a-date",
		"2025-02-26",
		"2025-02-29T00:00:00Z",
		"2025-04-31T00:00:00Z",
		"2025-13-01T00:00:00Z",
		"2025-01-00T00:00:00Z",
		"2025-01-01T24:00:00Z",
		"2025-01-01T00:60:00Z",
		"2025-01-01T00:00:60Z",
		"2025-01-01 00:00:00Z",
		"2025-01-01T00:00:00",
		"2025-01-01T00:00:00z",
	])
		expect(parseIsoUtc(raw)).toBeUndefined();
});

test("ISO Go parser fractional and whitespace compatibility", () => {
	expect(parseIsoUtc("\u00852025-02-26T12:34:56Z\u0085")).toBe(1740573296000);
	expect(parseIsoUtc("\ufeff2025-02-26T12:34:56Z")).toBeUndefined();
	expect(parseIsoUtc("2025-02-26T12:34:56,123456789Z")).toBe(1740573296123);
	expect(parseIsoUtc("2025-02-26T2:34:56Z")).toBe(1740537296000);
	expect(parseIsoUtc("2025-02-26T12:34:56+24:00")).toBe(1740486896000);
	expect(parseIsoUtc("2025-02-26T12:34:56+00:60")).toBe(1740569696000);
	expect(parseIsoUtc("2025-02-26T12:34:56+25:00")).toBeUndefined();
	expect(parseIsoUtc("2025-02-26T12:34:56+00:61")).toBeUndefined();
});
