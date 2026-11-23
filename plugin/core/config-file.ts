import { joinPath } from "./paths";
import type { FileReader } from "./runtime";

export async function defaultConfigPath(
	files: FileReader,
	home: string,
): Promise<string> {
	if (home === "") return "";
	const current = joinPath(home, ".ailinerc.toml");
	if ((await files.stat(current)) !== undefined) return current;
	const legacy = joinPath(home, ".claudelinerc.toml");
	if ((await files.stat(legacy)) !== undefined) return legacy;
	return current;
}
