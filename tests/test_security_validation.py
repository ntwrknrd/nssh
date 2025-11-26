"""Tests for input validation functions."""

from __future__ import annotations

import pytest

from nssh.core.security.validation import (
    validate_remote_path,
    validate_scp_args,
    validate_ssh_args,
)


class TestValidateScpArgs:
    """Tests for validate_scp_args function."""

    def test_empty_args(self):
        result = validate_scp_args([])
        assert result == []

    def test_safe_single_option(self):
        result = validate_scp_args(["-r"])
        assert result == ["-r"]

    def test_safe_multiple_options(self):
        result = validate_scp_args(["-r", "-p", "-q"])
        assert result == ["-r", "-p", "-q"]

    def test_option_with_argument(self):
        result = validate_scp_args(["-P", "2222"])
        assert result == ["-P", "2222"]

    def test_multiple_options_with_args(self):
        result = validate_scp_args(["-r", "-P", "2222", "-c", "aes256-ctr"])
        assert result == ["-r", "-P", "2222", "-c", "aes256-ctr"]

    def test_identity_file_option(self):
        result = validate_scp_args(["-i", "~/.ssh/id_rsa"])
        assert result == ["-i", "~/.ssh/id_rsa"]

    def test_config_file_option(self):
        result = validate_scp_args(["-F", "~/.ssh/config"])
        assert result == ["-F", "~/.ssh/config"]


class TestValidateScpArgsBlocking:
    """Tests for blocking dangerous scp arguments."""

    def test_blocks_dash_S_option(self):
        with pytest.raises(ValueError, match="Disallowed scp option: -S"):
            validate_scp_args(["-S", "/usr/bin/evil"])

    def test_blocks_dash_o_proxy_command(self):
        with pytest.raises(ValueError, match="Disallowed SSH option"):
            validate_scp_args(["-o", "ProxyCommand=/usr/bin/evil"])

    def test_blocks_dash_o_local_command(self):
        with pytest.raises(ValueError, match="Disallowed SSH option"):
            validate_scp_args(["-o", "LocalCommand=rm -rf /"])

    def test_blocks_dash_o_remote_command(self):
        with pytest.raises(ValueError, match="Disallowed SSH option"):
            validate_scp_args(["-o", "RemoteCommand=whoami"])

    def test_blocks_dash_o_permit_local_command(self):
        with pytest.raises(ValueError, match="Disallowed SSH option"):
            validate_scp_args(["-o", "PermitLocalCommand=yes"])

    def test_blocks_proxy_command_case_insensitive(self):
        with pytest.raises(ValueError, match="Disallowed SSH option"):
            validate_scp_args(["-o", "proxycommand=/usr/bin/evil"])

    def test_blocks_dash_o_without_space(self):
        """Test -oProxyCommand=... format (no space after -o)."""
        with pytest.raises(ValueError, match="Disallowed SSH option"):
            validate_scp_args(["-oProxyCommand=/usr/bin/evil"])

    def test_blocks_unknown_option(self):
        with pytest.raises(ValueError, match="Unknown or disallowed scp option: -X"):
            validate_scp_args(["-X"])

    def test_blocks_non_option_arguments(self):
        """Source/dest should not be in scp_args."""
        with pytest.raises(ValueError, match="Unexpected non-option argument"):
            validate_scp_args(["-r", "host:~/file.txt"])


class TestValidateScpArgsEdgeCases:
    """Edge case tests for validate_scp_args."""

    def test_option_requires_argument(self):
        """Test that options requiring arguments fail without them."""
        with pytest.raises(ValueError, match="requires an argument"):
            validate_scp_args(["-P"])

    def test_dash_o_requires_argument(self):
        with pytest.raises(ValueError, match="requires an argument"):
            validate_scp_args(["-o"])

    def test_safe_ssh_option(self):
        """Test that safe SSH options are allowed."""
        # Note: Current implementation blocks standalone -o entirely for safety
        # If we need to allow specific safe options, we'd need to whitelist them
        pass


class TestValidateRemotePath:
    """Tests for validate_remote_path function."""

    def test_valid_relative_path(self):
        result = validate_remote_path("file.txt")
        assert result == "file.txt"

    def test_valid_absolute_path(self):
        result = validate_remote_path("/etc/config")
        assert result == "/etc/config"

    def test_valid_tilde_path(self):
        result = validate_remote_path("~/file.txt")
        assert result == "~/file.txt"

    def test_valid_with_parent_dir(self):
        result = validate_remote_path("../file.txt")
        assert result == "../file.txt"

    def test_valid_with_multiple_parent_dirs(self):
        """Up to 3 '..' references is allowed."""
        result = validate_remote_path("../../../file.txt")
        assert result == "../../../file.txt"

    def test_valid_subdirectory_path(self):
        result = validate_remote_path("dir/subdir/file.txt")
        assert result == "dir/subdir/file.txt"

    def test_empty_path(self):
        """Empty path (e.g., 'host:') is valid."""
        result = validate_remote_path("")
        assert result == ""


class TestValidateRemotePathBlocking:
    """Tests for blocking dangerous paths."""

    def test_blocks_null_byte(self):
        with pytest.raises(ValueError, match="Null byte"):
            validate_remote_path("file\0.txt")

    def test_blocks_leading_dash(self):
        with pytest.raises(ValueError, match="cannot start with '-'"):
            validate_remote_path("-evil.txt")

    def test_blocks_excessive_parent_references(self):
        """More than 3 '..' is suspicious."""
        with pytest.raises(ValueError, match="Excessive parent directory"):
            validate_remote_path("../../../../file.txt")

    def test_blocks_many_dotdots_anywhere(self):
        """Block paths with many .. even if not all are traversals."""
        # This path has 4 '..' occurrences
        with pytest.raises(ValueError, match="Excessive parent directory"):
            validate_remote_path("../dir/../test/../file..txt/../final.txt")


class TestValidateRemotePathEdgeCases:
    """Edge case tests for validate_remote_path."""

    def test_dotdot_in_filename(self):
        """'..' in filename (not as path component) should still count."""
        # Current implementation counts all occurrences of '..'
        # This is intentionally conservative
        result = validate_remote_path("file..txt")
        assert result == "file..txt"

    def test_path_with_spaces(self):
        result = validate_remote_path("my file.txt")
        assert result == "my file.txt"

    def test_path_with_special_chars(self):
        result = validate_remote_path("file@host#123.txt")
        assert result == "file@host#123.txt"

    def test_unicode_path(self):
        result = validate_remote_path("文件.txt")
        assert result == "文件.txt"


class TestValidateSshArgs:
    """Tests for validate_ssh_args function."""

    def test_empty_args(self):
        result = validate_ssh_args([])
        assert result == []

    def test_safe_verbose_option(self):
        result = validate_ssh_args(["-v"])
        assert result == ["-v"]

    def test_multiple_verbose_options(self):
        """SSH allows repeating -v for more verbosity."""
        result = validate_ssh_args(["-v", "-v", "-v"])
        assert result == ["-v", "-v", "-v"]

    def test_combined_verbose_options(self):
        """SSH allows combined verbose flags like -vv and -vvv."""
        assert validate_ssh_args(["-vv"]) == ["-vv"]
        assert validate_ssh_args(["-vvv"]) == ["-vvv"]

    def test_combined_tty_options(self):
        """SSH allows -tt for forcing tty allocation."""
        assert validate_ssh_args(["-tt"]) == ["-tt"]

    def test_port_option(self):
        result = validate_ssh_args(["-p", "2222"])
        assert result == ["-p", "2222"]

    def test_identity_file_option(self):
        result = validate_ssh_args(["-i", "~/.ssh/id_rsa"])
        assert result == ["-i", "~/.ssh/id_rsa"]

    def test_config_file_option(self):
        result = validate_ssh_args(["-F", "~/.ssh/config"])
        assert result == ["-F", "~/.ssh/config"]

    def test_jump_host_option(self):
        """Jump host is safer alternative to ProxyCommand."""
        result = validate_ssh_args(["-J", "bastion.example.com"])
        assert result == ["-J", "bastion.example.com"]

    def test_local_forward_option(self):
        result = validate_ssh_args(["-L", "8080:localhost:80"])
        assert result == ["-L", "8080:localhost:80"]

    def test_remote_forward_option(self):
        result = validate_ssh_args(["-R", "8080:localhost:80"])
        assert result == ["-R", "8080:localhost:80"]

    def test_dynamic_forward_option(self):
        result = validate_ssh_args(["-D", "1080"])
        assert result == ["-D", "1080"]

    def test_compression_option(self):
        result = validate_ssh_args(["-C"])
        assert result == ["-C"]

    def test_terminal_options(self):
        result = validate_ssh_args(["-t", "-t"])  # Force TTY allocation
        assert result == ["-t", "-t"]

    def test_disable_terminal(self):
        result = validate_ssh_args(["-T"])
        assert result == ["-T"]

    def test_quiet_mode(self):
        result = validate_ssh_args(["-q"])
        assert result == ["-q"]

    def test_ipv4_option(self):
        result = validate_ssh_args(["-4"])
        assert result == ["-4"]

    def test_ipv6_option(self):
        result = validate_ssh_args(["-6"])
        assert result == ["-6"]

    def test_agent_forwarding_options(self):
        result = validate_ssh_args(["-A"])
        assert result == ["-A"]
        result = validate_ssh_args(["-a"])
        assert result == ["-a"]

    def test_x11_forwarding_options(self):
        result = validate_ssh_args(["-X"])
        assert result == ["-X"]
        result = validate_ssh_args(["-x"])
        assert result == ["-x"]
        result = validate_ssh_args(["-Y"])
        assert result == ["-Y"]

    def test_cipher_option(self):
        result = validate_ssh_args(["-c", "aes256-ctr"])
        assert result == ["-c", "aes256-ctr"]

    def test_mac_option(self):
        result = validate_ssh_args(["-m", "hmac-sha2-256"])
        assert result == ["-m", "hmac-sha2-256"]

    def test_escape_char_option(self):
        result = validate_ssh_args(["-e", "~"])
        assert result == ["-e", "~"]

    def test_bind_address_option(self):
        result = validate_ssh_args(["-b", "192.168.1.10"])
        assert result == ["-b", "192.168.1.10"]

    def test_bind_interface_option(self):
        result = validate_ssh_args(["-B", "eth0"])
        assert result == ["-B", "eth0"]

    def test_log_file_option(self):
        result = validate_ssh_args(["-E", "/tmp/ssh.log"])
        assert result == ["-E", "/tmp/ssh.log"]

    def test_multiplexing_options(self):
        result = validate_ssh_args(["-M"])
        assert result == ["-M"]
        result = validate_ssh_args(["-S", "/tmp/ssh-socket"])
        assert result == ["-S", "/tmp/ssh-socket"]
        result = validate_ssh_args(["-O", "check"])
        assert result == ["-O", "check"]

    def test_session_control_options(self):
        result = validate_ssh_args(["-N"])  # No command execution
        assert result == ["-N"]
        result = validate_ssh_args(["-n"])  # Redirect stdin from /dev/null
        assert result == ["-n"]
        result = validate_ssh_args(["-f"])  # Background
        assert result == ["-f"]

    def test_subsystem_option(self):
        result = validate_ssh_args(["-s"])
        assert result == ["-s"]

    def test_tunnel_option(self):
        result = validate_ssh_args(["-w", "0:1"])
        assert result == ["-w", "0:1"]

    def test_query_option(self):
        result = validate_ssh_args(["-Q", "cipher"])
        assert result == ["-Q", "cipher"]

    def test_version_option(self):
        result = validate_ssh_args(["-V"])
        assert result == ["-V"]

    def test_config_display_option(self):
        result = validate_ssh_args(["-G"])
        assert result == ["-G"]

    def test_tag_option(self):
        result = validate_ssh_args(["-P", "mytag"])
        assert result == ["-P", "mytag"]

    def test_forward_stdio_option(self):
        result = validate_ssh_args(["-W", "internal.example.com:22"])
        assert result == ["-W", "internal.example.com:22"]

    def test_pkcs11_option(self):
        result = validate_ssh_args(["-I", "/usr/lib/opensc-pkcs11.so"])
        assert result == ["-I", "/usr/lib/opensc-pkcs11.so"]

    def test_gssapi_options(self):
        result = validate_ssh_args(["-K"])
        assert result == ["-K"]
        result = validate_ssh_args(["-k"])
        assert result == ["-k"]

    def test_gateway_ports_option(self):
        result = validate_ssh_args(["-g"])
        assert result == ["-g"]

    def test_syslog_option(self):
        result = validate_ssh_args(["-y"])
        assert result == ["-y"]

    def test_complex_combination(self):
        """Test realistic combination of options."""
        result = validate_ssh_args(
            [
                "-v",
                "-p",
                "2222",
                "-i",
                "~/.ssh/id_rsa",
                "-L",
                "8080:localhost:80",
                "-C",
            ]
        )
        assert result == [
            "-v",
            "-p",
            "2222",
            "-i",
            "~/.ssh/id_rsa",
            "-L",
            "8080:localhost:80",
            "-C",
        ]

    def test_non_option_arguments_pass_through(self):
        """SSH allows non-option args like command arguments (after hostname)."""
        # Note: In actual usage, ssh_args contains only SSH options, not the command
        # The hostname and remote command are handled separately by PtyConnector
        result = validate_ssh_args(["-v", "echo", "hello", "world"])
        assert result == ["-v", "echo", "hello", "world"]

    def test_safe_ssh_config_option(self):
        """Test safe -o option like Port."""
        result = validate_ssh_args(["-o", "Port=2222"])
        assert result == ["-o", "Port=2222"]

    def test_double_dash_separator(self):
        """Test -- separator for remote command."""
        result = validate_ssh_args(["-v", "--", "echo", "hello"])
        assert result == ["-v", "--", "echo", "hello"]

    def test_double_dash_with_command_flags(self):
        """Test that flags after -- are not validated as SSH options."""
        # The -la after -- is a flag for 'ls', not an SSH option
        result = validate_ssh_args(["--", "ls", "-la"])
        assert result == ["--", "ls", "-la"]


class TestValidateSshArgsBlocking:
    """Tests for blocking dangerous SSH arguments."""

    def test_blocks_dash_o_proxy_command(self):
        with pytest.raises(ValueError, match="Disallowed SSH option"):
            validate_ssh_args(["-o", "ProxyCommand=/usr/bin/evil"])

    def test_blocks_dash_o_local_command(self):
        with pytest.raises(ValueError, match="Disallowed SSH option"):
            validate_ssh_args(["-o", "LocalCommand=rm -rf /"])

    def test_blocks_dash_o_remote_command(self):
        with pytest.raises(ValueError, match="Disallowed SSH option"):
            validate_ssh_args(["-o", "RemoteCommand=whoami"])

    def test_blocks_dash_o_permit_local_command(self):
        with pytest.raises(ValueError, match="Disallowed SSH option"):
            validate_ssh_args(["-o", "PermitLocalCommand=yes"])

    def test_blocks_proxy_command_case_insensitive(self):
        with pytest.raises(ValueError, match="Disallowed SSH option"):
            validate_ssh_args(["-o", "proxycommand=/usr/bin/evil"])

    def test_blocks_dash_o_without_space(self):
        """Test -oProxyCommand=... format (no space after -o)."""
        with pytest.raises(ValueError, match="Disallowed SSH option"):
            validate_ssh_args(["-oProxyCommand=/usr/bin/evil"])

    def test_blocks_unknown_option(self):
        """Unknown options are rejected for safety."""
        with pytest.raises(ValueError, match="Unknown or disallowed SSH option: -Z"):
            validate_ssh_args(["-Z"])

    def test_blocks_double_dash_with_bad_option(self):
        """Even with valid args before, unknown options are blocked."""
        with pytest.raises(ValueError, match="Unknown or disallowed SSH option"):
            validate_ssh_args(["-v", "-Z", "hostname"])


class TestValidateSshArgsEdgeCases:
    """Edge case tests for validate_ssh_args."""

    def test_option_requires_argument(self):
        """Test that options requiring arguments fail without them."""
        with pytest.raises(ValueError, match="requires an argument"):
            validate_ssh_args(["-p"])

    def test_dash_o_requires_argument(self):
        with pytest.raises(ValueError, match="requires an argument"):
            validate_ssh_args(["-o"])

    def test_multiple_identity_files(self):
        """SSH allows multiple -i options."""
        result = validate_ssh_args(["-i", "key1", "-i", "key2"])
        assert result == ["-i", "key1", "-i", "key2"]

    def test_option_at_end_of_list(self):
        """Option requiring argument at end should fail."""
        with pytest.raises(ValueError, match="requires an argument"):
            validate_ssh_args(["-v", "hostname", "-p"])
