import argparse
import hashlib
import io
from pathlib import Path
import platform
import urllib.request
import zipfile

VERSION = "1.4.5"
RUNTIMES = {
    "bun-darwin-arm64": ("bun-darwin-aarch64.zip", "b5d6bc2e13caf14e57c89661e89efd073ff16165b41bfe43882616410609cd10"),
    "bun-darwin-x64-baseline": ("bun-darwin-x64-baseline.zip", "fb0121739a7e80e7511b6bfe4d2116422b48ba18a013acadef0c425348e95891"),
    "bun-linux-arm64": ("bun-linux-aarch64.zip", "18656a3bf0ea5641f5796ae4213b75255579a70a0fded2f337933785568cdf84"),
    "bun-linux-x64-baseline": ("bun-linux-x64-baseline.zip", "dcb23a18a1c2fff12d57bcef696dbbb42eb67fde29f43816ecb595459a24c463"),
    "bun-linux-arm64-musl": ("bun-linux-aarch64-musl.zip", "d655765d3a0c0fdf707e27c7aa1a4f2c84f1bbc1f759db7e6e3d3c5be414396c"),
    "bun-linux-x64-musl-baseline": ("bun-linux-x64-musl-baseline.zip", "96fb94108eed872ff623ed39007d6b5699a83a5d0086fcfba84921c18145e3e6"),
}
MAX_ARCHIVE_BYTES = 128 * 1024 * 1024
MAX_EXECUTABLE_BYTES = 256 * 1024 * 1024


def install_runtime(output: Path, data: bytes, sha256: str, member: str) -> None:
    if hashlib.sha256(data).hexdigest() != sha256:
        raise ValueError("Bun archive checksum mismatch")
    with zipfile.ZipFile(io.BytesIO(data)) as package:
        info = package.getinfo(member)
        if info.file_size > MAX_EXECUTABLE_BYTES:
            raise ValueError("Bun executable exceeds size limit")
        executable = package.read(info)
    with output.open("xb") as stream:
        stream.write(executable)
    output.chmod(0o755)


def native_target() -> str:
    system = {"Darwin": "darwin", "Linux": "linux"}.get(platform.system())
    arch = {"arm64": "arm64", "aarch64": "arm64", "x86_64": "x64"}.get(platform.machine())
    if system is None or arch is None:
        raise ValueError("unsupported Bun compiler host")
    abi = "-musl" if system == "linux" and list(Path("/lib").glob("ld-musl-*.so.1")) else ""
    return f"bun-{system}-{arch}{abi}{'-baseline' if arch == 'x64' else ''}"


def prepare(directory: Path, archives: Path | None = None, native_only: bool = False) -> None:
    host = native_target()
    directory.mkdir(mode=0o700, parents=True)
    for target, (asset, sha256) in RUNTIMES.items():
        if native_only and target != host:
            continue
        if archives is not None:
            with (archives / asset).open("rb") as stream:
                data = stream.read(MAX_ARCHIVE_BYTES + 1)
        else:
            url = f"https://github.com/oven-sh/bun/releases/download/bun-v{VERSION}/{asset}"
            with urllib.request.urlopen(url, timeout=60) as response:
                data = response.read(MAX_ARCHIVE_BYTES + 1)
        if len(data) > MAX_ARCHIVE_BYTES:
            raise ValueError("Bun archive exceeds size limit")
        install_runtime(directory / target, data, sha256, f"{asset.removesuffix('.zip')}/bun")
    (directory / "bun").symlink_to(host)


def main() -> None:
    parser = argparse.ArgumentParser(description="Prepare checksum-pinned Bun compiler runtimes")
    parser.add_argument("directory", type=Path, help="New output directory; existing directories are refused")
    parser.add_argument("--archives", type=Path, help="Use previously downloaded official ZIP files")
    parser.add_argument("--native-only", action="store_true", help="Prepare only the compiler host runtime")
    args = parser.parse_args()
    prepare(args.directory.resolve(), args.archives, args.native_only)
    print(args.directory.resolve())


if __name__ == "__main__":
    main()
