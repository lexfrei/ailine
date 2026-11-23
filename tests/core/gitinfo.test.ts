import { expect, test } from "bun:test";
import {
	currentBranch,
	linkedWorktreeName,
	readGitInfo,
} from "../../plugin/core/gitinfo";
import type { FileReader } from "../../plugin/core/runtime";

function memoryFiles(
	entries: Record<string, string | "directory" | undefined>,
): FileReader {
	return {
		async stat(path) {
			const value = entries[path];
			if (value === undefined) return undefined;
			return {
				kind: value === "directory" ? "directory" : "file",
				size: new TextEncoder().encode(value).length,
				mtimeMs: 0,
			};
		},
		async read(path) {
			const value = entries[path];
			return value === undefined || value === "directory"
				? undefined
				: new TextEncoder().encode(value);
		},
	};
}

test("regular repo branch, detached HEAD, unreadable HEAD and no ancestor discovery", async () => {
	const fs = memoryFiles({
		"/repo/.git": "directory",
		"/repo/.git/HEAD": "\u0085ref: refs/heads/feat/foo\n",
	});
	expect(await currentBranch(fs, "/repo///")).toBe("feat/foo");
	expect(await linkedWorktreeName(fs, "/repo")).toBe("");
	for (const cwd of ["", "/missing", "/repo/child"]) {
		expect(await currentBranch(fs, cwd)).toBe("");
		expect(await linkedWorktreeName(fs, cwd)).toBe("");
	}
	for (const head of [
		undefined,
		"abcdef\n",
		"ref: refs/tags/v1",
		"﻿ref: refs/heads/main",
	]) {
		expect(
			await currentBranch(
				memoryFiles({ "/repo/.git": "directory", "/repo/.git/HEAD": head }),
				"/repo",
			),
		).toBe("");
	}
});

test("absolute and relative pointers preserve visible basename rather than metadata collision suffix", async () => {
	for (const pointer of [
		"/main/.git/worktrees/side1",
		"../../main/.git/worktrees/side1",
	]) {
		const fs = memoryFiles({
			"/other/side/.git": `\u0085gitdir: ${pointer}\n`,
			"/main/.git/worktrees/side1/HEAD": "ref: refs/heads/side\r\n",
		});
		expect(await readGitInfo(fs, "/other/side/")).toEqual({
			branch: "side",
			worktree: "side",
		});
		expect(await linkedWorktreeName(fs, "/other/side/")).toBe("side");
		expect(await currentBranch(fs, "/other/side/")).toBe("side");
	}
	// Go filepath.Dir keeps the metadata basename when the pointer ends in a slash.
	const trailing = memoryFiles({
		"/side/.git": "gitdir: /main/.git/worktrees/side1///",
		"/main/.git/worktrees/side1/HEAD": "ref: refs/heads/side",
	});
	expect(await readGitInfo(trailing, "/side")).toEqual({
		branch: "side",
		worktree: "",
	});
	const relative = memoryFiles({
		"side/.git": "gitdir: ../main/.git/worktrees/side1",
		"main/.git/worktrees/side1/HEAD": "ref: refs/heads/relative",
	});
	expect(await readGitInfo(relative, "side")).toEqual({
		branch: "relative",
		worktree: "side",
	});
});

test("strict pointer prefix, malformed layouts and unreadable pointers", async () => {
	for (const pointer of [
		"",
		"gitdir:",
		"gitdir: ",
		"gitdir:/meta",
		"gitdir:\t/meta",
		"garbage",
		"﻿gitdir: /meta",
	]) {
		expect(
			await readGitInfo(memoryFiles({ "/side/.git": pointer }), "/side"),
		).toEqual({ branch: "", worktree: "" });
	}
	const unreadable: FileReader = {
		async stat() {
			return { kind: "file", size: 10, mtimeMs: 0 };
		},
		async read() {
			return undefined;
		},
	};
	expect(await readGitInfo(unreadable, "/side")).toEqual({
		branch: "",
		worktree: "",
	});
	const fs = memoryFiles({
		"/side/.git": "gitdir: /custom/meta",
		"/custom/meta/HEAD": "ref: refs/heads/main",
	});
	expect(await readGitInfo(fs, "/side")).toEqual({
		branch: "main",
		worktree: "",
	});
	expect(
		await linkedWorktreeName(
			memoryFiles({ "/side/.git": "gitdir: /main/.git/worktrees/name" }),
			"/side",
		),
	).toBe("side");
});

test("POSIX backslashes are literal and combined reader resolves metadata once", async () => {
	const base = memoryFiles({
		"/side\\name/.git": "gitdir: /meta/worktrees/name",
		"/meta/worktrees/name/HEAD": "ref: refs/heads/main",
	});
	const reads: string[] = [];
	const stats: string[] = [];
	const fs: FileReader = {
		async stat(path) {
			stats.push(path);
			return base.stat(path);
		},
		async read(path) {
			reads.push(path);
			return base.read(path);
		},
	};
	expect(await readGitInfo(fs, "/side\\name")).toEqual({
		branch: "main",
		worktree: "side\\name",
	});
	expect(stats).toEqual(["/side\\name/.git"]);
	expect(reads).toEqual(["/side\\name/.git", "/meta/worktrees/name/HEAD"]);
});
