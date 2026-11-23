import { expect, test } from "bun:test";
import { mkdtemp, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const main = resolve(import.meta.dir, "../../cmd/ailine/main.ts");
const minimal =
	'[segments]\nstatus = false\nquota = false\ncredits = false\ncompactions = false\nrepo = false\nworktree = false\ncost = "false"\n';
const raw =
	'{"model":{"display_name":"Opus"},"context_window":{"used_percentage":37}}';
const quiet = [
	"--no-status",
	"--no-quota",
	"--no-credits",
	"--no-compactions",
	"--no-repo",
	"--no-worktree",
	"--cost=false",
];

type Files = {
	current?: string | undefined;
	legacy?: string | undefined;
	columns?: string | undefined;
};

async function cli(
	args: readonly string[],
	input = raw,
	files: Files = { current: minimal },
) {
	const home = await mkdtemp(join(tmpdir(), "ailine-main-test-"));
	try {
		if (files.current !== undefined)
			await writeFile(join(home, ".ailinerc.toml"), files.current);
		if (files.legacy !== undefined)
			await writeFile(join(home, ".claudelinerc.toml"), files.legacy);
		const child = Bun.spawn({
			cmd: [process.execPath, main, ...quiet, ...args],
			cwd: home,
			env: { PATH: "/usr/bin:/bin", HOME: home, COLUMNS: files.columns ?? "0" },
			stdin: new TextEncoder().encode(input),
			stdout: "pipe",
			stderr: "pipe",
		});
		const [stdout, stderr, exitCode] = await Promise.all([
			new Response(child.stdout).text(),
			new Response(child.stderr).text(),
			child.exited,
		]);
		return { stdout, stderr, exitCode };
	} finally {
		await rm(home, { recursive: true });
	}
}

test("real CLI consumes stdin and writes exact statusline bytes without a newline", async () => {
	expect(await cli([])).toEqual({
		exitCode: 0,
		stdout: "🤖 Opus | \x1b[32m🧠 37%\x1b[0m",
		stderr: "",
	});
});

test("empty stdin degrades silently and malformed nonempty stdin warns safely", async () => {
	expect(await cli([], "")).toEqual({
		exitCode: 0,
		stdout: "🤖 Claude",
		stderr: "",
	});
	const result = await cli([], '{"private-test-data":');
	expect(result.exitCode).toBe(0);
	expect(result.stdout).toBe("🤖 Claude");
	expect(result.stderr).toContain("ailine: stdin parse error:");
	expect(result.stderr).not.toContain("private-test-data");
});

test("CLI flags override config and preserve Changed semantics on no-* flags", async () => {
	const result = await cli(["--no-model=false", "--theme=text"]);
	expect(result).toEqual({
		exitCode: 0,
		stdout: "\x1b[32m37%\x1b[0m",
		stderr: "",
	});
});

test("current config wins and legacy-only config still loads", async () => {
	const legacy = `${minimal}model = false\n`;
	expect((await cli([], raw, { legacy })).stdout).toBe("\x1b[32m🧠 37%\x1b[0m");
	expect((await cli([], raw, { current: minimal, legacy })).stdout).toBe(
		"🤖 Opus | \x1b[32m🧠 37%\x1b[0m",
	);
});

test("validate reports unknown config keys and nonzero status", async () => {
	const result = await cli(["validate"], "", {
		current: "[segments]\nmodle = false\n",
	});
	expect(result.exitCode).toBe(1);
	expect(result.stdout).toBe("");
	expect(result.stderr).toContain(
		'error: unknown key "segments.modle" (typo?)',
	);
});

test("validate success and missing file differ", async () => {
	expect(await cli(["validate"])).toEqual({
		exitCode: 0,
		stdout: "config ok\n",
		stderr: "",
	});
	const result = await cli(["validate"], "", {});
	expect(result.exitCode).toBe(1);
	expect(result.stderr).toContain("error: cannot read config:");
});

test("help, version and completion do not read stdin or run the renderer", async () => {
	const version = await cli(["--version"], "");
	expect(version).toEqual({
		exitCode: 0,
		stdout: "ailine dev (none)\n",
		stderr: "",
	});
	const help = await cli(["--help"], "");
	expect(help.exitCode).toBe(0);
	expect(help.stdout).toContain("Usage:");
	expect(help.stdout).toContain("--per-model-quota");
	expect(help.stdout).not.toContain("--no-offpeak");
	const completion = await cli(["completion", "bash"], "");
	expect(completion.exitCode).toBe(0);
	expect(completion.stdout).toContain("ailine");
	expect(completion.stdout).not.toContain("🤖");
});

for (const args of [
	["--unknown"],
	["unexpected"],
	["validate", "extra"],
	["--per-model-quota", "auto"],
]) {
	test(`invalid CLI input ${args.join(" ")} fails without rendering`, async () => {
		const result = await cli(args);
		expect(result.exitCode).toBe(1);
		expect(result.stdout).toBe("");
		expect(result.stderr).toContain("Error:");
	});
}

test("terminal columns use a strict integer and subtract the safety margin", async () => {
	expect((await cli([], raw, { current: minimal, columns: "12" })).stdout).toBe(
		"🤖 Opus\n\x1b[32m🧠 37%\x1b[0m",
	);
	expect(
		(await cli([], raw, { current: minimal, columns: "12bad" })).stdout,
	).toBe("🤖 Opus | \x1b[32m🧠 37%\x1b[0m");
});

test("unreadable stdin prints a terminated error line instead of rendering", async () => {
	const home = await mkdtemp(join(tmpdir(), "ailine-main-test-"));
	try {
		const child = Bun.spawn({
			cmd: [
				"/bin/sh",
				"-c",
				'exec "$0" "$@" < "$HOME"',
				process.execPath,
				main,
				...quiet,
			],
			cwd: home,
			env: { PATH: "/usr/bin:/bin", HOME: home, COLUMNS: "0" },
			stdout: "pipe",
			stderr: "pipe",
		});
		const [stdout, exitCode] = await Promise.all([
			new Response(child.stdout).text(),
			child.exited,
		]);
		expect({ stdout, exitCode }).toEqual({
			stdout: "⚠️ stdin error\n",
			exitCode: 0,
		});
	} finally {
		await rm(home, { recursive: true });
	}
});
