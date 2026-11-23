import { baseName, isAbsolutePath, joinPath, parentPath } from "./paths";
import type { FileReader } from "./runtime";
import { decodeUtf8, trimSpace } from "./text";

type GitDirectory = { path: string; linked: boolean };

async function resolveGitDir(
	fs: FileReader,
	cwd: string,
): Promise<GitDirectory | undefined> {
	if (cwd === "") return undefined;
	const path = joinPath(cwd, ".git");
	const info = await fs.stat(path);
	if (info === undefined) return undefined;
	if (info.kind === "directory") return { path, linked: false };
	if (info.kind !== "file") return undefined;
	const bytes = await fs.read(path);
	if (bytes === undefined) return undefined;
	const line = trimSpace(decodeUtf8(bytes));
	if (!line.startsWith("gitdir: ")) return undefined;
	const target = line.slice("gitdir: ".length);
	return {
		path: isAbsolutePath(target) ? target : joinPath(cwd, target),
		linked: true,
	};
}

async function readBranch(fs: FileReader, gitDir: string): Promise<string> {
	const bytes = await fs.read(joinPath(gitDir, "HEAD"));
	if (bytes === undefined) return "";
	const line = trimSpace(decodeUtf8(bytes));
	const prefix = "ref: refs/heads/";
	return line.startsWith(prefix) ? line.slice(prefix.length) : "";
}

function worktreeName(gitDir: GitDirectory, cwd: string): string {
	if (!gitDir.linked || baseName(parentPath(gitDir.path)) !== "worktrees")
		return "";
	return baseName(cwd);
}

export async function currentBranch(
	fs: FileReader,
	cwd: string,
): Promise<string> {
	const gitDir = await resolveGitDir(fs, cwd);
	if (gitDir === undefined) return "";
	return readBranch(fs, gitDir.path);
}

export async function linkedWorktreeName(
	fs: FileReader,
	cwd: string,
): Promise<string> {
	const gitDir = await resolveGitDir(fs, cwd);
	if (gitDir === undefined) return "";
	return worktreeName(gitDir, cwd);
}

export async function readGitInfo(
	fs: FileReader,
	cwd: string,
): Promise<{ branch: string; worktree: string }> {
	const gitDir = await resolveGitDir(fs, cwd);
	if (gitDir === undefined) return { branch: "", worktree: "" };
	return {
		branch: await readBranch(fs, gitDir.path),
		worktree: worktreeName(gitDir, cwd),
	};
}
