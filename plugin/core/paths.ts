export function isAbsolutePath(path: string): boolean {
	return path.startsWith("/");
}

export function joinPath(...parts: readonly string[]): string {
	const path = parts.filter((part) => part !== "").join("/");
	if (path === "") return "";
	const absolute = isAbsolutePath(path);
	const elements: string[] = [];
	for (const part of path.split("/")) {
		if (part === "" || part === ".") continue;
		if (part === ".." && elements.length > 0 && elements.at(-1) !== "..") {
			elements.pop();
		} else if (part !== ".." || !absolute) {
			elements.push(part);
		}
	}
	const result = (absolute ? "/" : "") + elements.join("/");
	return result || ".";
}

export function baseName(path: string): string {
	if (path === "") return ".";
	const stripped = path.replace(/\/+$/u, "");
	if (stripped === "") return "/";
	return stripped.slice(stripped.lastIndexOf("/") + 1);
}

export function parentPath(path: string): string {
	const end = path.lastIndexOf("/");
	if (end < 0) return ".";
	return joinPath(path.slice(0, end + 1));
}
