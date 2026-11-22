import { parse } from "smol-toml";
import stringWidth from "string-width";

export function parseToml(source: string): Record<string, unknown> {
	return parse(source, { integersAsBigInt: true });
}

export function measureWidth(text: string): number {
	return stringWidth(text, { countAnsiEscapeCodes: true });
}
