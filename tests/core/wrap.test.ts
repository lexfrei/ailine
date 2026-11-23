import { expect, test } from "bun:test";
import {
	joinPipeWrap,
	stripFormatting,
	visualWidth,
} from "../../plugin/core/wrap";

test("native text strips colors and both hyperlink terminators", () => {
	expect(stripFormatting("\x1b[32m🧠 50%\x1b[0m")).toBe("🧠 50%");
	for (const end of ["\x07", "\x1b\\"]) {
		expect(
			stripFormatting(`\x1b]8;;https://example.com${end}#42\x1b]8;;${end}`),
		).toBe("#42");
	}
	expect(stripFormatting("plain\ntext")).toBe("plain\ntext");
});

test("visible width Go table with BEL and ST hyperlinks", () => {
	for (const [text, width] of [
		["hello", 5],
		["🤖", 2],
		["🤖 Opus", 7],
		["\x1b[32m🧠 50%\x1b[0m", 6],
		["\x1b]8;;https://example.com\x07#42\x1b]8;;\x07", 3],
		["\x1b]8;;https://example.com\x1b\\#42\x1b]8;;\x1b\\", 3],
		["界", 2],
		["é", 1],
		["", 0],
	] as const)
		expect(visualWidth(text)).toBe(width);
});
test("wrap fallback, exact fit, overflow, oversized segments and Go empty-segment behavior", () => {
	for (const [parts, width, want] of [
		[[], 80, ""],
		[["a"], 80, "a"],
		[["a", "b", "c"], 80, "a | b | c"],
		[["a", "b", "c"], 0, "a | b | c"],
		[["a", "b", "c"], -1, "a | b | c"],
		[["aaaaa", "bbbbb", "ccccc"], 9, "aaaaa | bbbbb | ccccc"],
		[["aaaaa", "bbbbb", "ccccc"], 14, "aaaaa | bbbbb\nccccc"],
		[["aaaaa", "bbbbb"], 13, "aaaaa | bbbbb"],
		[["🤖 a", "🐙 b"], 10, "🤖 a\n🐙 b"],
		[["a", "longerthanwidth", "b"], 10, "a\nlongerthanwidth\nb"],
		[["", "a"], 10, "a"],
		[["a", ""], 10, "a | "],
		[["", ""], 10, ""],
		[["a", "", "b"], 10, "a |  | b"],
	] as [string[], number, string][])
		expect(joinPipeWrap(parts, width)).toBe(want);
});
test("mixed statusline and colored hyperlinks pack without splitting a segment", () => {
	const parts = [
		"🤖 Opus 4.7 ⏫💭",
		"🧠 67%",
		"🔄 2",
		"🟡 7d: 42%",
		"🔴 5h: 91%",
	];
	const result = joinPipeWrap(parts, 30);
	expect(result).toBe(
		"🤖 Opus 4.7 ⏫💭 | 🧠 67%\n🔄 2 | 🟡 7d: 42% | 🔴 5h: 91%",
	);
	for (const line of result.split("\n"))
		expect(visualWidth(line)).toBeLessThanOrEqual(30);
	const linked =
		"\x1b[32m\x1b]8;;https://example.com\x07#42\x1b]8;;\x07\x1b[0m";
	expect(joinPipeWrap([linked, "abcd"], 10)).toBe(`${linked} | abcd`);
});
