import { expect, test } from "bun:test";
import { decodeUtf8, toWellFormed } from "../../plugin/core/text";

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

test("UTF-16 normalization matches the standard method for every code unit", () => {
	for (let code = 0; code <= 0xffff; code++) {
		const value = String.fromCharCode(code);
		expect(toWellFormed(value)).toBe(value.toWellFormed());
	}
});

for (const value of [
	"",
	"plain",
	"😀",
	"a\ud800😀\udfffz",
	"\ud800𐏿\udfff",
	"\udfff\ud800",
	"﻿\u0085",
]) {
	test(`UTF-16 normalization retains valid pairs: ${JSON.stringify(value)}`, () => {
		expect(toWellFormed(value)).toBe(value.toWellFormed());
	});
}
