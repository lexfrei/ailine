import { cp, mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

export async function testHostRuntime(): Promise<number> {
	const root = resolve(import.meta.dir, "..");
	const directory = await mkdtemp(join(tmpdir(), "ailine-host-runtime-"));
	try {
		await cp(join(root, "tests/host-runtime"), directory, { recursive: true });
		await cp(join(root, "plugin/core"), join(directory, "core"), {
			recursive: true,
		});
		await cp(join(root, "plugin/vendor"), join(directory, "vendor"), {
			recursive: true,
		});
		await cp(
			join(root, "plugin/hooks/runtime.ts"),
			join(directory, "hooks/runtime.ts"),
		);
		return await Bun.spawn(["claude", "plugin", "test", directory], {
			cwd: root,
			stdout: "inherit",
			stderr: "inherit",
		}).exited;
	} finally {
		await rm(directory, { recursive: true, force: true });
	}
}

if (import.meta.main) process.exitCode = await testHostRuntime();
