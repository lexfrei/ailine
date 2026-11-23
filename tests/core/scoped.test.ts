import { expect, test } from "bun:test";
import {
	matchesModel,
	matchesScopedModel,
	mostBinding,
	perModelCandidates,
	perModelWindows,
	scopedLabel,
} from "../../plugin/core/scoped";
import type {
	QuotaData,
	QuotaWindow,
	ScopedWindow,
} from "../../plugin/core/types";
const win = (utilization: number): QuotaWindow => ({
	utilization,
	resetsAtMs: 0,
	totalMinutes: 10080,
	remainingMinutes: 0,
});
const bucket = (name: string, utilization: number, id = ""): ScopedWindow => ({
	name,
	id,
	window: win(utilization),
});

test("model matching Go table and version boundaries", () => {
	for (const [id, display, name, want] of [
		["claude-fable-5", "Fable 5", "Fable", true],
		["claude-opus-4-8", "Opus 4.8", "opus", true],
		["claude-sonnet-5", "Sonnet 5", "sonnet", true],
		["claude-opus-4-8", "Opus 4.8", "Fable", false],
		["", "Sonnet 5", "sonnet", true],
		["claude-sonnet-5", "Sonnet 5", "", false],
		["claude-opus-4-8", "Opus 4.8", "oauth", false],
		["claude-fable-5", "Fable 5", "Fable 5", true],
		["claude-opus-4-5", "Opus 4.5", "Claude Opus 4.5", true],
		["", "Opus 4.5", "Opus 4.5", true],
		["claude-sonnet-4-5-20250929", "Sonnet 4.5", "Claude Sonnet 4", false],
		["claude-sonnet-4-5-20250929", "Sonnet 4.5", "Claude Sonnet 4.5", true],
		["claude-sonnet-4-5", "Sonnet 4.5", "sonnet", true],
		["claude-sonnet-4-10", "", "sonnet 4.1", false],
		["claude-sonnet-4-10", "", "sonnet 4", false],
		["claude-sonnet-4-100", "", "sonnet 4", true],
		["claude-sonnetish-5", "", "sonnet", false],
		[" CLAUDE_OPUS_4_5 ", "", " Claude Opus 4.5 ", true],
		["claude-sonnet-٤-٥", "", "sonnet ٤", false],
		["claude-sonnet--4", "", "sonnet 4", false],
		["", "", "sonnet", false],
	] as const)
		expect(matchesModel(id, display, name)).toBe(want);
});
test("scope ID overrides display names and accepts date suffix in either direction", () => {
	for (const [id, bucketID, name, display, want] of [
		["claude-fable-5", "claude-fable-5", "Other", "", true],
		["claude-sonnet-4-5", "claude-sonnet-4", "sonnet", "Sonnet 4.5", false],
		["claude-fable-5", "", "Fable", "Fable 5", true],
		["claude-sonnet-4-5-20250929", "claude-sonnet-4-5", "Sonnet 4.5", "", true],
		["claude-sonnet-4-5", "claude-sonnet-4-5-20250929", "Sonnet 4.5", "", true],
		["", "unrelated", "Sonnet", "Sonnet 5", true],
	] as const)
		expect(
			matchesScopedModel(id, display, {
				name,
				id: bucketID,
				window: undefined,
			}),
		).toBe(want);
});
test("labels normalize only the separators Go normalizes", () => {
	for (const [name, label] of [
		["Fable", "7d-fable"],
		["Fable 5", "7d-fable-5"],
		["opus", "7d-opus"],
		[" Fable ", "7d-fable"],
		["Claude_Opus 4.5", "7d-claude-opus-4-5"],
		["", "7d-"],
		["A  B", "7d-a--b"],
		["A\tB", "7d-a\tb"],
	] as const)
		expect(scopedLabel(name ?? "")).toBe(label);
});
test("candidates preserve collisions; collapsed labels keep highest utilization and first tie", () => {
	const data: QuotaData = {
		sevenDayOpus: win(10),
		sevenDaySonnet: win(30),
		sevenDayCowork: win(40),
		sevenDayOAuthApps: win(20),
		scoped: [
			bucket("Fable", 100),
			bucket("Opus", 10),
			{ name: "empty", id: "", window: undefined },
		],
	};
	expect(perModelCandidates(data).map((w) => w.name)).toEqual([
		"opus",
		"sonnet",
		"cowork",
		"oauth",
		"Fable",
		"Opus",
	]);
	expect(perModelWindows(data).map((w) => scopedLabel(w.name))).toEqual([
		"7d-opus",
		"7d-sonnet",
		"7d-cowork",
		"7d-oauth",
		"7d-fable",
	]);
	for (const [a, b, want] of [
		[20, 96, 96],
		[96, 20, 96],
		[50, 50, 50],
	] as const) {
		const result = perModelWindows({
			sevenDaySonnet: win(a),
			scoped: [bucket("Sonnet", b)],
		});
		expect(result).toHaveLength(1);
		expect(result[0]?.window?.utilization).toBe(want);
		if (a === b) expect(result[0]?.name).toBe("sonnet");
	}
	expect(
		perModelWindows({
			sevenDaySonnet: win(10),
			scoped: [bucket("Sonnet 4.5", 20)],
		}),
	).toHaveLength(2);
	expect(perModelWindows(undefined)).toEqual([]);
	expect(perModelCandidates(undefined)).toEqual([]);
	expect(perModelWindows({})).toEqual([]);
});
test("filter before collapsing prevents a foreign model bucket shadowing the family bucket", () => {
	const data = {
		sevenDayOpus: win(20),
		scoped: [bucket("Opus", 96, "claude-opus-4-5")],
	};
	const filtered = perModelCandidates(data).filter((w) =>
		matchesScopedModel("claude-opus-4-8", "Opus 4.8", w),
	);
	expect(mostBinding(filtered)).toEqual([
		{ name: "opus", id: "", window: win(20) },
	]);
});
test("most binding highest utilization, first ties, missing windows skipped", () => {
	for (const [input, name] of [
		[[bucket("sonnet", 30), bucket("Sonnet 4.5", 60)], "Sonnet 4.5"],
		[[bucket("sonnet", 95), bucket("Sonnet 4.5", 30)], "sonnet"],
		[[bucket("sonnet", 50), bucket("Sonnet 4.5", 50)], "sonnet"],
		[[bucket("Fable", 10)], "Fable"],
		[
			[{ name: "nil", id: "", window: undefined }, bucket("Fable", 10)],
			"Fable",
		],
	] as [ScopedWindow[], string][])
		expect(mostBinding(input).map((w) => w.name)).toEqual([name]);
	expect(mostBinding([])).toEqual([]);
	expect(mostBinding([{ name: "nil", id: "", window: undefined }])).toEqual([]);
});

test("normalization matches Go whitespace instead of JavaScript BOM trimming", () => {
	expect(scopedLabel("\u0085Fable\u0085")).toBe("7d-fable");
	expect(scopedLabel("\ufeffFable")).toBe("7d-\ufefffable");
});
