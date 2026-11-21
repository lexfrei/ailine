import { expect, test } from "bun:test";
import { decodeUtf8 } from "../../plugin/core/text";

for (const prefixes of [0, 1, 2]) {
	test(`UTF-8 decoding preserves ${prefixes} leading BOMs`, () => {
		const expected = `${String.fromCodePoint(0xfeff).repeat(prefixes)}ref: refs/heads/main\n`;
		expect(decodeUtf8(new TextEncoder().encode(expected))).toBe(expected);
	});
}

test("UTF-8 decoding supports empty data and non-ASCII text", () => {
	expect(decodeUtf8(new Uint8Array())).toBe("");
	expect(decodeUtf8(new TextEncoder().encode("📦 café界"))).toBe("📦 café界");
});
