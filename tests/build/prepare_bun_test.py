import hashlib
import importlib.util
import io
from pathlib import Path
import tempfile
import sys
import unittest
from unittest.mock import patch
import zipfile

sys.dont_write_bytecode = True

spec = importlib.util.spec_from_file_location("prepare_bun", Path(__file__).resolve().parents[2] / "tools/prepare_bun.py")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


def archive(member="bun-fixture/bun", content=b"fixture executable"):
    output = io.BytesIO()
    with zipfile.ZipFile(output, "w") as package:
        package.writestr(member, content)
    return output.getvalue()


class PrepareBunTest(unittest.TestCase):
    def test_verified_member_is_written_as_executable(self):
        data = archive()
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "bun-fixture"
            module.install_runtime(output, data, hashlib.sha256(data).hexdigest(), "bun-fixture/bun")
            self.assertEqual(output.read_bytes(), b"fixture executable")
            self.assertEqual(output.stat().st_mode & 0o777, 0o755)

    def test_wrong_checksum_is_rejected_before_zip_parsing_or_writing(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "bun-fixture"
            with self.assertRaisesRegex(ValueError, "checksum"):
                module.install_runtime(output, b"not a ZIP", "0" * 64, "bun-fixture/bun")
            self.assertFalse(output.exists())

    def test_existing_output_is_not_overwritten(self):
        data = archive()
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "bun-fixture"
            output.write_bytes(b"existing")
            with self.assertRaises(FileExistsError):
                module.install_runtime(output, data, hashlib.sha256(data).hexdigest(), "bun-fixture/bun")
            self.assertEqual(output.read_bytes(), b"existing")

    def test_only_exact_member_is_read_without_extracting_other_paths(self):
        data = archive("../../unrelated")
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "bun-fixture"
            with self.assertRaises(KeyError):
                module.install_runtime(output, data, hashlib.sha256(data).hexdigest(), "bun-fixture/bun")
            self.assertEqual(list(Path(directory).iterdir()), [])

    def test_native_compiler_target_matches_supported_hosts(self):
        for system, machine, musl, expected in [
            ("Darwin", "arm64", False, "bun-darwin-arm64"),
            ("Darwin", "x86_64", False, "bun-darwin-x64-baseline"),
            ("Linux", "aarch64", False, "bun-linux-arm64"),
            ("Linux", "x86_64", False, "bun-linux-x64-baseline"),
            ("Linux", "aarch64", True, "bun-linux-arm64-musl"),
            ("Linux", "x86_64", True, "bun-linux-x64-musl-baseline"),
        ]:
            with self.subTest(system=system, machine=machine, musl=musl):
                with patch.object(module.platform, "system", return_value=system), patch.object(module.platform, "machine", return_value=machine), patch.object(module.Path, "glob", return_value=[Path("/lib/ld-musl.so.1")] if musl else []):
                    self.assertEqual(module.native_target(), expected)

    def test_unsupported_compiler_host_is_rejected(self):
        for system, machine in [("Windows", "x86_64"), ("Linux", "riscv64")]:
            with patch.object(module.platform, "system", return_value=system), patch.object(module.platform, "machine", return_value=machine):
                with self.assertRaisesRegex(ValueError, "unsupported"):
                    module.native_target()

    def test_existing_directory_is_refused_without_altering_contents(self):
        with tempfile.TemporaryDirectory() as directory:
            sentinel = Path(directory) / "existing"
            sentinel.write_bytes(b"unchanged")
            with self.assertRaises(FileExistsError):
                module.prepare(Path(directory))
            self.assertEqual(sentinel.read_bytes(), b"unchanged")

    def test_archive_size_is_bounded_before_installation(self):
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            (base / "fixture.zip").write_bytes(b"12345")
            with patch.object(module, "RUNTIMES", {"bun-fixture": ("fixture.zip", "0" * 64)}), patch.object(module, "MAX_ARCHIVE_BYTES", 4):
                with self.assertRaisesRegex(ValueError, "archive exceeds"):
                    module.prepare(base / "output", base)
            self.assertEqual(list((base / "output").iterdir()), [])

    def test_local_archives_prepare_compiler_symlink_without_network(self):
        data = archive()
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            (base / "bun-fixture.zip").write_bytes(data)
            with patch.object(module, "RUNTIMES", {"bun-fixture": ("bun-fixture.zip", hashlib.sha256(data).hexdigest())}), patch.object(module, "native_target", return_value="bun-fixture"), patch.object(module.urllib.request, "urlopen") as network:
                module.prepare(base / "output", base)
                network.assert_not_called()
            self.assertEqual((base / "output/bun").read_bytes(), b"fixture executable")
            self.assertEqual((base / "output").stat().st_mode & 0o777, 0o700)

    def test_download_uses_fixed_release_and_bounded_read(self):
        data = archive()
        with tempfile.TemporaryDirectory() as directory:
            with patch.object(module, "RUNTIMES", {"bun-fixture": ("bun-fixture.zip", hashlib.sha256(data).hexdigest())}), patch.object(module, "native_target", return_value="bun-fixture"), patch.object(module.urllib.request, "urlopen") as network:
                response = network.return_value.__enter__.return_value
                response.read.return_value = data
                module.prepare(Path(directory) / "output")
                network.assert_called_once_with("https://github.com/oven-sh/bun/releases/download/bun-v1.4.5/bun-fixture.zip", timeout=60)
                response.read.assert_called_once_with(module.MAX_ARCHIVE_BYTES + 1)

    def test_executable_size_is_bounded_before_writing(self):
        data = archive()
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "bun-fixture"
            with patch.object(module, "MAX_EXECUTABLE_BYTES", 1):
                with self.assertRaisesRegex(ValueError, "executable exceeds"):
                    module.install_runtime(output, data, hashlib.sha256(data).hexdigest(), "bun-fixture/bun")
            self.assertFalse(output.exists())

    def test_native_only_skips_other_archives(self):
        data = archive()
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            (base / "bun-fixture.zip").write_bytes(data)
            runtimes = {"bun-fixture": ("bun-fixture.zip", hashlib.sha256(data).hexdigest()), "other": ("must-not-read.zip", "0" * 64)}
            with patch.object(module, "RUNTIMES", runtimes), patch.object(module, "native_target", return_value="bun-fixture"):
                module.prepare(base / "output", base, native_only=True)
            self.assertEqual(sorted(path.name for path in (base / "output").iterdir()), ["bun", "bun-fixture"])

    def test_six_targets_are_pinned_with_baseline_x64(self):
        self.assertEqual(len(module.RUNTIMES), 6)
        for target, (asset, checksum) in module.RUNTIMES.items():
            self.assertTrue(target.startswith("bun-"))
            self.assertTrue(asset.endswith(".zip"))
            self.assertRegex(checksum, r"^[a-f0-9]{64}$")
            if "x64" in target:
                self.assertTrue(target.endswith("-baseline"))


if __name__ == "__main__":
    unittest.main()
