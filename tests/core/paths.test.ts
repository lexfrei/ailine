import { expect, test } from "bun:test";
import {
	baseName,
	isAbsolutePath,
	joinPath,
	parentPath,
} from "../../plugin/core/paths";

test("lexical POSIX joining cleans dots and separators without changing backslashes", () => {
	for (const [parts, expected] of [
		[[], ""],
		[["", ""], ""],
		[["a", "", "b/"], "a/b"],
		[["/a//b", "../c", "."], "/a/c"],
		[["a", "/b"], "a/b"],
		[["/", "../../a"], "/a"],
		[["a", "../../b"], "../b"],
		[["a", ".."], "."],
		[["a\\b", "c"], "a\\b/c"],
	] as const)
		expect(joinPath(...parts)).toBe(expected);
});

test("basename and parent follow POSIX lexical paths", () => {
	for (const [path, base, parent] of [
		["", ".", "."],
		["/", "/", "/"],
		["///", "/", "/"],
		["a", "a", "."],
		["/a/b///", "b", "/a/b"],
		["a/../b", "b", "."],
		["a\\b", "a\\b", "."],
	] as const) {
		expect(baseName(path)).toBe(base);
		expect(parentPath(path)).toBe(parent);
	}
	expect(isAbsolutePath("/a")).toBe(true);
	for (const path of ["", "a", "C:\\a", "\\a"])
		expect(isAbsolutePath(path)).toBe(false);
});
