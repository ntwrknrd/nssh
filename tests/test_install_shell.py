from __future__ import annotations

from typer.testing import CliRunner

from nssh.cli.install_shell import app


def test_install_shell_dry_run(tmp_path):
    runner = CliRunner()
    result = runner.invoke(
        app,
        [
            f"--bin-dir={tmp_path / 'bin'}",
            f"--share-dir={tmp_path / 'share'}",
            f"--shell-profile={tmp_path / 'profile'}",
            f"--fish-functions-dir={tmp_path / 'fish_func'}",
            f"--fish-completions-dir={tmp_path / 'fish_comp'}",
            "--dry-run",
        ],
    )

    assert result.exit_code == 0, result.output
    assert "nssh install-shell" in result.output
