import { expect, test } from "bun:test";
import { formatFixed } from "../../plugin/core/format";

const cases = [
	[0, 2, "0.00"],
	[-0, 2, "-0.00"],
	[0.125, 2, "0.12"],
	[0.375, 2, "0.38"],
	[2.5, 0, "2"],
	[3.5, 0, "4"],
	[-2.5, 0, "-2"],
	[-3.5, 0, "-4"],
	[1.005, 2, "1.00"],
	[1.015, 2, "1.01"],
	[1000, 2, "1000.00"],
	[1e21, 2, "1000000000000000000000.00"],
	[Number.MIN_VALUE, 2, "0.00"],
	[100.999, 2, "101.00"],
] as const;

for (const [value, digits, expected] of cases) {
	test(`formats finite amount ${Object.is(value, -0) ? "-0" : value} at ${digits} digits`, () => {
		expect(formatFixed(value, digits)).toBe(expected);
	});
}

test("rejects non-finite amounts rather than emitting misleading money", () => {
	expect(() => formatFixed(Number.NaN, 2)).toThrow();
	expect(() => formatFixed(Number.POSITIVE_INFINITY, 2)).toThrow();
});
