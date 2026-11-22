import { expect, test } from "bun:test";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { resolve, join } from "node:path";
import { pathToFileURL } from "node:url";
import { buildVendor } from "../../tools/build-vendor";

const projectRoot = resolve(import.meta.dir, "../..");

test("vendor artifacts are readable, typed, licensed and independent of npm at runtime", async () => {
	const output = await mkdtemp(join(tmpdir(), "ailine-vendor-test-"));
	try {
		await buildVendor(projectRoot, output);
		const code = await readFile(join(output, "runtime.js"), "utf8");
		const declarations = await readFile(join(output, "runtime.d.ts"), "utf8");
		const notices = await readFile(
			join(output, "THIRD_PARTY_NOTICES.txt"),
			"utf8",
		);
		expect(code).not.toContain(projectRoot);
		expect(code).not.toContain("node:");
		expect(code).not.toContain('from "smol-toml"');
		expect(code).not.toContain('from "string-width"');
		expect(declarations).not.toContain("import");
		expect(declarations).toContain("parseToml");
		expect(declarations).toContain("measureWidth");
		for (const name of [
			"smol-toml",
			"string-width",
			"strip-ansi",
			"ansi-regex",
			"get-east-asian-width",
		])
			expect(notices).toContain(name);
		expect(notices).toContain("Redistribution and use");
		expect(notices).toContain("Permission is hereby granted");
		expect(notices.endsWith("\n")).toBe(true);
		expect(notices.endsWith("\n\n")).toBe(false);
		const runtime = await import(
			pathToFileURL(join(output, "runtime.js")).href
		);
		expect(runtime.parseToml("value = 9007199254740993").value).toBe(
			9007199254740993n,
		);
		expect(runtime.measureWidth("📦 café界")).toBe(9);
	} finally {
		await rm(output, { recursive: true });
	}
}, 15000);
