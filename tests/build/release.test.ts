import { expect, test } from "bun:test";
import {
	mkdtemp,
	mkdir,
	readFile,
	rm,
	symlink,
	writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const root = resolve(import.meta.dir, "../..");

test("release configuration selects Bun and all six reviewed baseline targets", async () => {
	const source = await readFile(join(root, ".goreleaser.yaml"), "utf8");
	expect(source).toContain("builder: bun");
	for (const target of [
		"darwin-arm64",
		"darwin-x64-baseline",
		"linux-arm64",
		"linux-x64-baseline",
		"linux-arm64-musl",
		"linux-x64-musl-baseline",
	]) {
		expect(source).toContain(`- ${target}\n`);
	}
	for (const kind of ["dotenv", "bunfig", "tsconfig", "package-json"])
		expect(source).toContain(`--no-compile-autoload-${kind}`);
	expect(source).toContain("--compile-executable-path=");
	expect(source).toContain("libc6 (>= 2.17)");
	expect(source).toContain(
		'doc.install "BUN_LICENSE.md", "THIRD_PARTY_NOTICES.txt"',
	);
});

async function execute(
	argv: string[],
	cwd: string,
	env: Record<string, string>,
	stdin = "",
) {
	const child = Bun.spawn(argv, {
		cwd,
		env,
		stdin: new TextEncoder().encode(stdin),
		stdout: "pipe",
		stderr: "pipe",
	});
	const [stdout, stderr, exitCode] = await Promise.all([
		new Response(child.stdout).text(),
		new Response(child.stderr).text(),
		child.exited,
	]);
	return { stdout, stderr, exitCode };
}

test("GoReleaser produces an isolated standalone CLI with embedded version and disabled autoload", async () => {
	const directory = await mkdtemp(join(tmpdir(), "ailine-release-test-"));
	try {
		const runtimes = join(directory, "runtimes");
		await mkdir(runtimes);
		const target = `bun-${process.platform}-${process.arch === "x64" ? "x64-baseline" : "arm64"}`;
		await symlink(process.execPath, join(runtimes, target));
		const source = await readFile(join(root, ".goreleaser.yaml"), "utf8");
		const config = join(directory, "goreleaser.yaml");
		await writeFile(
			config,
			`dist: ${JSON.stringify(join(directory, "dist"))}\n${source}`,
		);
		const binary = join(directory, "ailine");
		const build = await execute(
			[
				"goreleaser",
				"build",
				"--config",
				config,
				"--snapshot",
				"--single-target",
				"--id",
				"ailine-standalone",
				"--output",
				binary,
			],
			root,
			{
				...process.env,
				BUN_RUNTIME_DIR: runtimes,
				TARGET: target.slice(4),
			},
		);
		expect(build.exitCode, build.stdout + build.stderr).toBe(0);
		const home = join(directory, "home");
		const cwd = join(directory, "workspace");
		await mkdir(home);
		await mkdir(cwd);
		const env = { PATH: "/usr/bin:/bin", HOME: home };
		const version = await execute([binary, "--version"], cwd, env);
		expect(version.exitCode).toBe(0);
		expect(version.stdout).toMatch(/^ailine \S+ \([a-f0-9]+\)\n$/);
		expect(version.stdout).not.toContain("dev (none)");
		await writeFile(join(cwd, ".env"), "COLUMNS=12\n");
		await writeFile(
			join(cwd, "bunfig.toml"),
			'preload = ["./must-not-run.ts"]\n',
		);
		await writeFile(
			join(cwd, "tsconfig.json"),
			'{"compilerOptions":{"paths":{"*": ["./must-not-run.ts"]}}}',
		);
		await writeFile(
			join(cwd, "package.json"),
			'{"type":"module","imports":{"#*":"./must-not-run.ts"}}',
		);
		await writeFile(
			join(cwd, "must-not-run.ts"),
			'throw new Error("unexpected runtime autoload")',
		);
		const rendered = await execute(
			[
				binary,
				"--no-status",
				"--no-quota",
				"--no-credits",
				"--no-compactions",
				"--no-repo",
				"--no-worktree",
				"--cost=false",
			],
			cwd,
			env,
			'{"model":{"display_name":"Opus"},"context_window":{"used_percentage":37}}',
		);
		expect(rendered).toEqual({
			exitCode: 0,
			stdout: "🤖 Opus | \x1b[32m🧠 37%\x1b[0m",
			stderr: "",
		});
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
}, 120000);
