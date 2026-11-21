import stringWidth from "string-width";

const ansiEscape = /\x1b\[[0-9;]*m|\x1b\]8;[^\x07\x1b]*(?:\x07|\x1b\\)/g;

export function visualWidth(text: string): number {
	return stringWidth(text.replace(ansiEscape, ""), {
		countAnsiEscapeCodes: true,
	});
}

export function joinPipeWrap(
	parts: readonly string[],
	maxWidth: number,
): string {
	if (maxWidth < 10 || parts.length === 0) return parts.join(" | ");
	const lines: string[] = [];
	let current = "";
	let currentWidth = 0;
	for (const segment of parts) {
		const width = visualWidth(segment);
		const fits = currentWidth === 0 || currentWidth + 3 + width <= maxWidth;
		if (!fits && current !== "") {
			lines.push(current);
			current = "";
			currentWidth = 0;
		}
		if (current !== "") {
			current += " | ";
			currentWidth += 3;
		}
		current += segment;
		currentWidth += width;
	}
	if (current !== "") lines.push(current);
	return lines.join("\n");
}
