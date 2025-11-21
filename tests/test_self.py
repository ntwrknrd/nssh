"""Tests for nssh self command."""

from __future__ import annotations

from typer.testing import CliRunner

from nssh import __version__
from nssh.cli.self import app as self_app
from nssh.cli.self.manifest import (
    InstallManifest,
    delete_manifest,
    read_manifest,
    write_manifest,
)


runner = CliRunner()


def test_manifest_roundtrip(tmp_path):
    """Test manifest can be written and read back."""
    manifest = InstallManifest()
    manifest.add_file(tmp_path / "test.txt", "file", "scripts/test.txt")
    manifest.add_file(tmp_path / "link", "symlink", None, tmp_path / "target")
    manifest.add_profile_modification(tmp_path / ".bashrc", "# >>> test >>>", 10, 15)

    write_manifest(manifest, tmp_path)
    loaded = read_manifest(tmp_path)

    assert loaded is not None
    assert len(loaded.files) == 2
    assert len(loaded.profile_modifications) == 1
    assert loaded.files[0].type == "file"
    assert loaded.files[1].type == "symlink"
    assert loaded.profile_modifications[0].marker == "# >>> test >>>"


def test_read_manifest_missing(tmp_path):
    """Test reading non-existent manifest returns None."""
    result = read_manifest(tmp_path)
    assert result is None


def test_delete_manifest(tmp_path):
    """Test manifest deletion."""
    manifest = InstallManifest()
    write_manifest(manifest, tmp_path)
    assert (tmp_path / "manifest.json").exists()

    delete_manifest(tmp_path)
    assert not (tmp_path / "manifest.json").exists()


def test_self_version_command_reports_package_version():
    """Version command surfaces the installed nssh version string."""

    result = runner.invoke(self_app, ["version"])

    assert result.exit_code == 0
    assert __version__ in result.stdout


def test_self_status_command_runs_in_isolated_env(tmp_path, monkeypatch):
    """Status command executes without touching the real system."""

    share_dir = tmp_path / "share"
    share_dir.mkdir(parents=True)

    config_path = tmp_path / "config.toml"
    config_path.write_text("[self]\n")

    monkeypatch.setenv("NSSH_SHARE_DIR", str(share_dir))
    monkeypatch.setenv("NSSH_CONFIG", str(config_path))
    monkeypatch.setattr("pathlib.Path.home", lambda: tmp_path)
    monkeypatch.setattr("nssh.cli.self.status.check_nssh_on_path", lambda: True)

    result = runner.invoke(self_app, ["status"])

    assert result.exit_code == 0
    assert "nssh self status" in result.stdout
