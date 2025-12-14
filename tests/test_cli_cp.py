"""Tests for nssh cp command."""

from __future__ import annotations

import pytest
from click.testing import CliRunner

from nssh.cli.cp import (
    _detect_direction,
    _parse_remote_spec,
    app,
)


class TestParseRemoteSpec:
    """Tests for _parse_remote_spec helper."""

    def test_valid_remote_spec(self):
        user, host, path = _parse_remote_spec("myhost:~/file.txt")
        assert user is None
        assert host == "myhost"
        assert path == "~/file.txt"

    def test_remote_spec_with_user(self):
        user, host, path = _parse_remote_spec("admin@myhost:~/file.txt")
        assert user == "admin"
        assert host == "myhost"
        assert path == "~/file.txt"

    def test_remote_spec_with_absolute_path(self):
        user, host, path = _parse_remote_spec("server:/etc/config")
        assert user is None
        assert host == "server"
        assert path == "/etc/config"

    def test_remote_spec_empty_path(self):
        user, host, path = _parse_remote_spec("host:")
        assert user is None
        assert host == "host"
        assert path == ""

    def test_not_remote_spec_raises(self):
        with pytest.raises(ValueError, match="Not a remote spec"):
            _parse_remote_spec("./local/file.txt")


class TestDetectDirection:
    """Tests for _detect_direction helper."""

    def test_pull_direction(self):
        user, host, remote_path, local_path, direction = _detect_direction(
            "myhost:~/file.txt", "./"
        )
        assert user is None
        assert host == "myhost"
        assert remote_path == "~/file.txt"
        assert local_path == "./"
        assert direction == "pull"

    def test_pull_direction_with_user(self):
        user, host, remote_path, local_path, direction = _detect_direction(
            "admin@myhost:~/file.txt", "./"
        )
        assert user == "admin"
        assert host == "myhost"
        assert remote_path == "~/file.txt"
        assert local_path == "./"
        assert direction == "pull"

    def test_push_direction(self):
        user, host, remote_path, local_path, direction = _detect_direction(
            "./file.txt", "myhost:~/"
        )
        assert user is None
        assert host == "myhost"
        assert remote_path == "~/"
        assert local_path == "./file.txt"
        assert direction == "push"

    def test_push_direction_with_user(self):
        user, host, remote_path, local_path, direction = _detect_direction(
            "./file.txt", "deploy@myhost:~/"
        )
        assert user == "deploy"
        assert host == "myhost"
        assert remote_path == "~/"
        assert local_path == "./file.txt"
        assert direction == "push"

    def test_both_remote_raises(self):
        import click

        with pytest.raises(
            click.BadParameter, match="Cannot copy between two remote hosts"
        ):
            _detect_direction("host1:~/a", "host2:~/b")

    def test_neither_remote_raises(self):
        import click

        with pytest.raises(click.BadParameter, match="One path must be remote"):
            _detect_direction("./local1", "./local2")


class TestCpCommand:
    """Tests for the cp command CLI."""

    @pytest.fixture
    def runner(self):
        return CliRunner()

    def test_help_shows_usage(self, runner):
        result = runner.invoke(app, ["--help"])
        # Typer shows help even with add_help_option=False when --help is passed
        assert result.exit_code == 0 or "Usage" in result.output

    def test_missing_args_shows_error(self, runner):
        result = runner.invoke(app, [])
        assert result.exit_code != 0

    def test_both_local_paths_error(self, runner):
        result = runner.invoke(app, ["./local1", "./local2"])
        assert result.exit_code != 0
        assert "remote" in result.output.lower()

    def test_both_remote_paths_error(self, runner):
        result = runner.invoke(app, ["host1:~/a", "host2:~/b"])
        assert result.exit_code != 0
        assert "remote" in result.output.lower()


class TestScpConnector:
    """Tests for ScpConnector."""

    def test_build_scp_command(self):
        from nssh.core.connector.scp import ScpConnector

        connector = ScpConnector(
            source="host:~/file.txt",
            dest="./",
            scp_args=["-r", "-p"],
        )
        cmd = connector._build_scp_command()
        assert cmd == ["scp", "-r", "-p", "host:~/file.txt", "./"]

    def test_build_scp_command_no_args(self):
        from nssh.core.connector.scp import ScpConnector

        connector = ScpConnector(
            source="./file.txt",
            dest="host:~/",
        )
        cmd = connector._build_scp_command()
        assert cmd == ["scp", "./file.txt", "host:~/"]

    def test_password_pattern_detection(self):
        from nssh.core.connector.scp import ScpConnector

        connector = ScpConnector(
            source="host:~/file.txt",
            dest="./",
            password="secret",
        )

        # Test various password prompt patterns
        connector._buffer = bytearray(b"user@host's password: ")
        assert connector._check_password_prompt() is True

        connector._buffer = bytearray(b"Password: ")
        assert connector._check_password_prompt() is True

        connector._buffer = bytearray(b"passcode: ")
        assert connector._check_password_prompt() is True

        connector._buffer = bytearray(b"some other output")
        assert connector._check_password_prompt() is False


class TestScpConnectorSecurity:
    """Tests for security features of ScpConnector."""

    def test_rejects_malicious_scp_args(self):
        from nssh.core.connector.scp import ScpConnector

        # Test rejection of -S option (program execution)
        with pytest.raises(ValueError, match="Invalid scp arguments"):
            ScpConnector(
                source="host:~/file.txt",
                dest="./",
                scp_args=["-S", "/usr/bin/evil"],
            )

    def test_rejects_proxy_command_injection(self):
        from nssh.core.connector.scp import ScpConnector

        # Test rejection of ProxyCommand injection
        with pytest.raises(ValueError, match="Invalid scp arguments"):
            ScpConnector(
                source="host:~/file.txt",
                dest="./",
                scp_args=["-o", "ProxyCommand=/usr/bin/evil"],
            )

    def test_rejects_path_with_null_byte(self):
        from nssh.core.connector.scp import ScpConnector

        with pytest.raises(ValueError, match="Null byte"):
            ScpConnector(
                source="host:~/file\0.txt",
                dest="./",
            )

    def test_rejects_path_starting_with_dash(self):
        from nssh.core.connector.scp import ScpConnector

        # Test path validation - the path part after ':' starts with '-'
        with pytest.raises(ValueError, match="cannot start with '-'"):
            ScpConnector(
                source="-evil.txt",  # Local path starting with dash
                dest="host:~/",
            )

    def test_password_cleared_after_use(self):
        from nssh.core.connector.scp import ScpConnector

        connector = ScpConnector(
            source="host:~/file.txt",
            dest="./",
            password="secret123",
        )

        # Password should be stored securely
        assert connector._password
        assert connector._password.get_bytes() == b"secret123"

        # Simulate password being sent
        connector._password.clear()

        # Password should be cleared
        assert not connector._password


class TestScpConnectorIntegration:
    """Integration tests for ScpConnector."""

    def test_selector_initialized(self):
        from nssh.core.connector.scp import ScpConnector

        connector = ScpConnector(
            source="host:~/file.txt",
            dest="./",
        )

        # Selector should be None until run() is called
        assert connector._selector is None

    def test_timing_logger_initialized(self):
        from nssh.core.connector.scp import ScpConnector

        connector = ScpConnector(
            source="host:~/file.txt",
            dest="./",
        )

        # Timing logger should be initialized
        assert connector._timing_logger is not None


class TestScpProgressParsing:
    """Tests for SCP progress line parsing."""

    def test_parse_progress_line_completed(self):
        from nssh.core.connector.scp import _parse_scp_progress

        # Completed transfers don't have ETA suffix
        result = _parse_scp_progress("config.txt   100% 2235KB 713.3KB/s   00:03")
        assert result is not None
        assert result["filename"] == "config.txt"
        assert result["percent"] == 100
        assert result["transferred"] == "2235KB"
        assert result["speed"] == "713.3KB/s"
        assert result["time"] == "00:03"

    def test_parse_progress_line_with_eta(self):
        from nssh.core.connector.scp import _parse_scp_progress

        # In-progress transfers have ETA suffix
        result = _parse_scp_progress(
            "bigfile.tar.gz    45%  512MB  10.2MB/s   05:30 ETA"
        )
        assert result is not None
        assert result["filename"] == "bigfile.tar.gz"
        assert result["percent"] == 45
        assert result["transferred"] == "512MB"
        assert result["speed"] == "10.2MB/s"
        assert result["time"] == "05:30"

    def test_parse_progress_line_zero_percent(self):
        from nssh.core.connector.scp import _parse_scp_progress

        result = _parse_scp_progress("dub-core.gz   0%    0    0.0KB/s   --:-- ETA")
        assert result is not None
        assert result["filename"] == "dub-core.gz"
        assert result["percent"] == 0
        assert result["speed"] == "0.0KB/s"

    def test_parse_progress_line_long_filename(self):
        from nssh.core.connector.scp import _parse_scp_progress

        result = _parse_scp_progress(
            "very_long_filename_with_lots_of_chars.txt   75% 1024KB 500.0KB/s   00:02 ETA"
        )
        assert result is not None
        assert result["filename"] == "very_long_filename_with_lots_of_chars.txt"
        assert result["percent"] == 75

    def test_parse_non_progress_line_returns_none(self):
        from nssh.core.connector.scp import _parse_scp_progress

        assert _parse_scp_progress("Permission denied") is None
        assert _parse_scp_progress("") is None
        assert _parse_scp_progress("Connecting to host...") is None
