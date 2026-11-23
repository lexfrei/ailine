import { expect, test } from "bun:test";
import { isRecord, parseJson } from "../../plugin/core/json";

test("JSON decoding preserves unknown values and recognizes records", () => {
	for (const raw of ['{"value":1}', "[]", "null", "true", '"text"', "1e999"])
		expect(parseJson(raw)).toEqual(JSON.parse(raw));
	expect(isRecord({})).toBe(true);
	for (const value of [null, [], "text", 0, undefined])
		expect(isRecord(value)).toBe(false);
});

test("JSON errors do not expose input or native causes", () => {
	for (const raw of ['{"secret-token":}', "secret-token", "", "{}{}"])
		try {
			parseJson(raw);
			expect.unreachable();
		} catch (error) {
			expect(error).toBeInstanceOf(Error);
			if (!(error instanceof Error)) throw error;
			expect(error.message).toBe("Invalid JSON payload");
			expect(error.cause).toBeUndefined();
		}
});

test("JSON depth guard matches the encoding/json ceiling without recursion", () => {
	expect(parseJson(`${"[".repeat(10000)}0${"]".repeat(10000)}`)).toBeArray();
	expect(() => parseJson(`${"[".repeat(10001)}0${"]".repeat(10001)}`)).toThrow(
		"JSON nesting limit exceeded",
	);
	expect(() =>
		parseJson(`${'{"x":'.repeat(10001)}0${"}".repeat(10001)}`),
	).toThrow("JSON nesting limit exceeded");
	expect(parseJson(JSON.stringify(`\\"${"[{".repeat(11000)}`))).toBe(
		`\\"${"[{".repeat(11000)}`,
	);
});
