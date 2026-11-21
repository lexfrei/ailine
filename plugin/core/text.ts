export function trimSpace(value: string): string {
	// String.trim() differs from Go TrimSpace for NEL and BOM.
	return value.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, "");
}
