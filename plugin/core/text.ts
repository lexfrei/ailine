export function trimSpace(value: string): string {
	// String.trim() differs from Go TrimSpace for NEL and BOM.
	return value.replace(/^\p{White_Space}+|\p{White_Space}+$/gu, "");
}

export function decodeUtf8(bytes: Uint8Array): string {
	let offset = 0;
	while (
		bytes[offset] === 0xef &&
		bytes[offset + 1] === 0xbb &&
		bytes[offset + 2] === 0xbf
	)
		offset += 3;
	const text = new TextDecoder("utf-8").decode(bytes.subarray(offset));
	return String.fromCodePoint(0xfeff).repeat(offset / 3) + text;
}
