import { expect, test } from "bun:test";
import { defaultConfigPath } from "../../plugin/core/config-file";
import type { FileReader } from "../../plugin/core/runtime";

for (const [existing, expected] of [
	[[], "/home/test-user/.ailinerc.toml"],
	[
		["/home/test-user/.claudelinerc.toml"],
		"/home/test-user/.claudelinerc.toml",
	],
	[["/home/test-user/.ailinerc.toml"], "/home/test-user/.ailinerc.toml"],
	[
		["/home/test-user/.ailinerc.toml", "/home/test-user/.claudelinerc.toml"],
		"/home/test-user/.ailinerc.toml",
	],
] as const) {
	test(`default config path prefers current then legacy: ${existing.join(",")}`, async () => {
		const files: FileReader = {
			async stat(path) {
				return existing.some((value) => value === path)
					? { kind: "file", size: 0, mtimeMs: 0 }
					: undefined;
			},
			async read() {
				throw new Error("path selection must not read config content");
			},
		};
		expect(await defaultConfigPath(files, "/home/test-user")).toBe(expected);
	});
}

test("missing home does not access the filesystem", async () => {
	const files: FileReader = {
		async stat() {
			throw new Error("unexpected stat");
		},
		async read() {
			throw new Error("unexpected read");
		},
	};
	expect(await defaultConfigPath(files, "")).toBe("");
});
