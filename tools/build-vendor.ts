import { mkdir, readFile, rename } from "node:fs/promises";
import { join, resolve } from "node:path";

const packages = [
	"smol-toml",
	"string-width",
	"strip-ansi",
	"ansi-regex",
	"get-east-asian-width",
] as const;

export async function buildVendor(
	projectRoot: string,
	outputDirectory = join(projectRoot, "plugin/vendor"),
): Promise<void> {
	await mkdir(outputDirectory, { recursive: true });
	const build = await Bun.build({
		root: projectRoot,
		entrypoints: [join(projectRoot, "tools/vendor.ts")],
		target: "browser",
		format: "esm",
		minify: false,
		sourcemap: "none",
	});
	const output = build.outputs[0];
	if (!build.success || output === undefined)
		throw new Error("Vendor bundle generation failed");
	await Bun.write(join(outputDirectory, "runtime.js"), output);
	const compiler = Bun.spawn({
		cmd: [
			process.execPath,
			join(projectRoot, "node_modules/typescript/bin/tsc"),
			"--target",
			"ESNext",
			"--module",
			"ESNext",
			"--moduleResolution",
			"Bundler",
			"--strict",
			"--skipLibCheck",
			"false",
			"--types",
			"bun",
			"--declaration",
			"--emitDeclarationOnly",
			"--outDir",
			outputDirectory,
			join(projectRoot, "tools/vendor.ts"),
		],
		cwd: projectRoot,
		stdout: "pipe",
		stderr: "pipe",
	});
	const [stdout, stderr, status] = await Promise.all([
		new Response(compiler.stdout).text(),
		new Response(compiler.stderr).text(),
		compiler.exited,
	]);
	if (status !== 0)
		throw new Error(`Vendor declarations failed: ${stdout}${stderr}`);
	await rename(
		join(outputDirectory, "vendor.d.ts"),
		join(outputDirectory, "runtime.d.ts"),
	);
	let notices = "Dependency license notices\n";
	for (const name of packages) {
		const directory = join(projectRoot, "node_modules", name);
		const metadata: unknown = JSON.parse(
			await readFile(join(directory, "package.json"), "utf8"),
		);
		const version =
			typeof metadata === "object" &&
			metadata !== null &&
			"version" in metadata &&
			typeof metadata.version === "string"
				? metadata.version
				: "unknown";
		let license: string | undefined;
		for (const filename of ["LICENSE", "LICENSE.md", "license", "license.md"]) {
			try {
				license = await readFile(join(directory, filename), "utf8");
				break;
			} catch {}
		}
		if (license === undefined) throw new Error(`Missing license for ${name}`);
		notices += `\n${name}@${version}\n${"=".repeat(name.length + version.length + 1)}\n${license}\n`;
	}
	await Bun.write(join(outputDirectory, "THIRD_PARTY_NOTICES.txt"), `${notices.trimEnd()}\n`);
}

if (import.meta.main) await buildVendor(resolve(import.meta.dir, ".."));
