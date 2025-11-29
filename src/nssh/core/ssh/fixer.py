"""Authentication + compatibility helpers extracted from legacy utils."""

from __future__ import annotations

import io
import re
from typing import Any, Dict, List

from nssh.core.auth.credentials import CredentialManager
from nssh.core.connector.pty import PtyConnector
from nssh.core.ssh.config import SSHConfigParser

AUTH_CONFIGS: Dict[str, Dict[str, Any]] = {
    "password": {
        "name": "Password authentication",
        "description": "PreferredAuthentications password",
        "config_lines": [
            "  PubkeyAuthentication no\n",
            "  PreferredAuthentications password\n",
        ],
        "detection_patterns": ["PreferredAuthentications password"],
    },
    "keyboard-interactive": {
        "name": "Keyboard-interactive authentication",
        "description": "PreferredAuthentications keyboard-interactive",
        "config_lines": [
            "  PubkeyAuthentication no\n",
            "  PreferredAuthentications keyboard-interactive\n",
        ],
        "detection_patterns": ["PreferredAuthentications keyboard-interactive"],
    },
    "publickey": {
        "name": "Public key authentication",
        "description": "PubkeyAuthentication yes",
        "config_lines": [
            "  PubkeyAuthentication yes\n",
            "  PasswordAuthentication no\n",
        ],
        "detection_patterns": ["PubkeyAuthentication yes"],
    },
}

AUTH_CONFIGS["pass"] = AUTH_CONFIGS["password"]
AUTH_CONFIGS["kbd"] = AUTH_CONFIGS["keyboard-interactive"]
AUTH_CONFIGS["key"] = AUTH_CONFIGS["publickey"]
AUTH_CONFIGS["kba"] = AUTH_CONFIGS["publickey"]

AUTH_DETECTION_PATTERNS = {
    "keyboard-interactive": re.compile(
        r"PreferredAuthentications\s+keyboard-interactive"
    ),
    "password": re.compile(r"PreferredAuthentications\s+password"),
    "publickey": re.compile(r"PubkeyAuthentication\s+yes"),
}

COMPAT_CONFIGS: Dict[str, Dict[str, Any]] = {
    "kex": {
        "name": "Legacy Key Exchange",
        "description": "Add legacy KexAlgorithms for older SSH servers",
        "config_lines": [
            "  KexAlgorithms +diffie-hellman-group1-sha1,diffie-hellman-group14-sha1,diffie-hellman-group-exchange-sha1,diffie-hellman-group-exchange-sha256\n",
        ],
        "detection_patterns": [r"KexAlgorithms\s+"],
        "error_patterns": [
            r"no matching key exchange method found",
            r"unable to negotiate [^:]+: no matching key exchange",
        ],
    },
    "macs": {
        "name": "Legacy MACs",
        "description": "Add legacy MAC algorithms for older SSH servers",
        "config_lines": [
            "  MACs +hmac-sha1,hmac-sha1-96,hmac-md5,hmac-md5-96\n",
        ],
        "detection_patterns": [r"MACs\s+"],
        "error_patterns": [
            r"no matching macs? found",
            r"unable to negotiate [^:]+: no matching mac",
        ],
    },
    "ciphers": {
        "name": "Legacy Ciphers",
        "description": "Add legacy cipher algorithms for older SSH servers",
        "config_lines": [
            "  Ciphers +aes128-cbc,3des-cbc,aes192-cbc,aes256-cbc\n",
        ],
        "detection_patterns": [r"Ciphers\s+"],
        "error_patterns": [
            r"no matching ciphers? found",
            r"unable to negotiate [^:]+: no matching cipher",
        ],
    },
    "hostkey": {
        "name": "Legacy Host Key Algorithms",
        "description": "Add legacy host key algorithms for older SSH servers",
        "config_lines": [
            "  HostKeyAlgorithms +ssh-rsa,ssh-dss\n",
        ],
        "detection_patterns": [r"HostKeyAlgorithms\s+"],
        "error_patterns": [
            r"no matching host key type found",
            r"unable to negotiate [^:]+: no matching host key",
        ],
    },
}


def parse_ssh_compatibility_error(stderr_text: str) -> List[str]:
    needed: List[str] = []
    for compat_type, config in COMPAT_CONFIGS.items():
        patterns = config.get("error_patterns") or [config.get("error_pattern", "")]
        for pattern in patterns:
            if pattern and re.search(pattern, stderr_text, re.IGNORECASE):
                needed.append(compat_type)
                break
    return needed


def _is_auth_failure_after_successful_kex(stderr_text: str) -> bool:
    """Check if connection failed on auth after successful key exchange.

    This indicates compatibility issues were resolved but authentication
    failed (expected when using BatchMode with password-only hosts).
    """
    has_successful_kex = bool(re.search(r"debug1:.*kex:.*algorithm:", stderr_text))
    has_auth_failure = bool(
        re.search(
            r"(Permission denied|No more authentication methods)",
            stderr_text,
            re.IGNORECASE,
        )
    )
    return has_successful_kex and has_auth_failure


def _extract_authenticated_method(stderr_text: str) -> str | None:
    match = re.search(
        r'Authenticated to [^\s]+(?:\s+\([^)]+\))?\s+using\s+"([^"]+)"',
        stderr_text,
        re.IGNORECASE,
    )
    if match:
        return match.group(1).lower()
    return None


def _did_authentication_succeed(stderr_text: str) -> bool:
    return bool(
        re.search(
            r"Authenticated to [^\s]+(?:\s+\([^)]+\))?", stderr_text, re.IGNORECASE
        )
    )


def test_ssh_connection_via_cli(
    hostname: str,
    timeout: int = 10,
    parser: SSHConfigParser | None = None,
) -> Dict[str, Any]:
    parser_obj = parser or SSHConfigParser()
    host_info = parser_obj.find_host_in_files(hostname)
    if not host_info:
        return {
            "success": False,
            "exit_code": 3,
            "stderr": f"Host '{hostname}' not found in SSH config",
            "stdout": "",
        }

    target_file, host_lines = host_info
    fields = extract_ssh_fields(host_lines)
    configured_user = fields.get("user") or None
    auth_type = detect_auth_type(host_lines)

    password_required = auth_type in {"password", "keyboard-interactive"}
    resolved_username = configured_user
    password: str | None = None
    credential_warning: str | None = None

    if password_required:
        try:
            cm = CredentialManager()
        except RuntimeError as exc:
            return {
                "success": False,
                "exit_code": 1,
                "stderr": str(exc),
                "stdout": "",
            }

        credential = cm.resolve_credential(
            hostname,
            git_include_file=target_file.name,
            username=configured_user,
        )

        if credential:
            resolved_username, password = credential
        else:
            credential_warning = (
                f"[nssh] No stored credential for host '{hostname}'. "
                "Running compatibility test in BatchMode (authentication will fail)."
            )

    batch_mode = password is None

    ssh_args = [
        "-vv",
        "-o",
        f"ConnectTimeout={timeout}",
        "-o",
        "NumberOfPasswordPrompts=1",
    ]
    if batch_mode:
        ssh_args.extend(["-o", "BatchMode=yes"])
    else:
        ssh_args.extend(
            [
                "-o",
                "PreferredAuthentications=password,keyboard-interactive,publickey",
            ]
        )
    ssh_args.extend(
        [
            "-o",
            "StrictHostKeyChecking=accept-new",
            "--",
            "exit",
        ]
    )

    output_buffer = io.BytesIO()

    try:
        connector = PtyConnector(
            hostname=hostname,
            username=resolved_username,
            password=password,
            ssh_args=ssh_args,
            stdout=output_buffer,
            attach_stdin=False,
        )
        exit_code = connector.run()
    except FileNotFoundError:
        return {
            "success": False,
            "exit_code": 127,
            "stderr": "ssh command not found in PATH",
            "stdout": "",
        }
    except Exception as exc:  # pragma: no cover - defensive fallback
        return {
            "success": False,
            "exit_code": 1,
            "stderr": str(exc),
            "stdout": "",
        }

    raw_output = output_buffer.getvalue().decode("utf-8", errors="ignore")
    auth_method = _extract_authenticated_method(raw_output)
    lowered = raw_output.lower()
    if "timed out" in lowered and exit_code != 0:
        return {
            "success": False,
            "exit_code": 124,
            "stderr": f"Connection timed out after {timeout} seconds",
            "stdout": raw_output,
        }

    if credential_warning:
        raw_output = (
            f"{credential_warning}\n{raw_output}" if raw_output else credential_warning
        )

    success = exit_code == 0
    if not success and _did_authentication_succeed(raw_output):
        success = True
        raw_output = (
            "[nssh] Remote CLI rejected probe command 'exit'; treating connection as successful.\n"
            f"{raw_output}"
        )

    return {
        "success": success,
        "exit_code": exit_code,
        "stderr": raw_output,
        "stdout": "",
        "auth_method": auth_method,
    }


def iterative_compatibility_fix(
    parser: SSHConfigParser,
    hostname: str,
    max_iterations: int = 5,
    verbose: bool = True,
) -> Dict[str, Any]:
    from nssh.core.ssh.mutations import HostUpdateError, apply_host_update

    console = None
    if verbose:
        from nssh.core.ui.console import get_console

        console = get_console()

    all_fixes_applied: List[str] = []

    for iteration in range(1, max_iterations + 1):
        test_result = test_ssh_connection_via_cli(
            hostname,
            timeout=10,
            parser=parser,
        )

        if test_result["success"]:
            if verbose and console and all_fixes_applied:
                console.print("[dim]Testing... success[/dim]")
            return {
                "success": True,
                "iterations": iteration,
                "fixes_applied": all_fixes_applied,
                "final_test_result": test_result,
                "stopped_reason": "connection_succeeded",
            }

        # Parse SSH verbose output (stderr) for compatibility errors
        compat_types = parse_ssh_compatibility_error(test_result["stderr"])

        if not compat_types:
            # Check if we succeeded at key exchange but failed at auth
            # This means compatibility issues are resolved (success!)
            if all_fixes_applied and _is_auth_failure_after_successful_kex(
                test_result["stderr"]
            ):
                if verbose and console:
                    console.print(
                        "[dim]Testing... success (KEX ok, auth skipped)[/dim]"
                    )
                return {
                    "success": True,
                    "iterations": iteration,
                    "fixes_applied": all_fixes_applied,
                    "final_test_result": test_result,
                    "stopped_reason": "auth_failed_after_kex_success",
                }

            if test_result["exit_code"] != 255:
                reason = {
                    124: "timeout",
                    127: "command_not_found",
                }.get(test_result["exit_code"], "non_compatibility_error")
                return {
                    "success": False,
                    "iterations": iteration,
                    "fixes_applied": all_fixes_applied,
                    "final_test_result": test_result,
                    "stopped_reason": reason,
                }
            return {
                "success": False,
                "iterations": iteration,
                "fixes_applied": all_fixes_applied,
                "final_test_result": test_result,
                "stopped_reason": "no_more_compatibility_issues",
            }

        new_fixes = [fix for fix in compat_types if fix not in all_fixes_applied]
        if not new_fixes:
            if verbose and console:
                console.print(
                    f"[yellow]![/yellow] Same compatibility issues persist: {', '.join(compat_types)}"
                )
            return {
                "success": False,
                "iterations": iteration,
                "fixes_applied": all_fixes_applied,
                "final_test_result": test_result,
                "stopped_reason": "no_progress",
            }

        if verbose and console:
            compat_names = [COMPAT_CONFIGS[fix]["name"] for fix in new_fixes]
            console.print(f"[dim]Testing... applying {', '.join(compat_names)}[/dim]")

        try:
            apply_host_update(
                parser,
                hostname,
                auth_type=None,
                compat_types=new_fixes,
            )
            all_fixes_applied.extend(new_fixes)
        except HostUpdateError as exc:
            if verbose and console:
                console.print(f"[red]Error applying fixes: {exc}[/red]")
            return {
                "success": False,
                "iterations": iteration,
                "fixes_applied": all_fixes_applied,
                "final_test_result": test_result,
                "stopped_reason": "fix_application_error",
            }
        except Exception as exc:  # pragma: no cover - defensive fallback
            if verbose and console:
                console.print(f"[red]Error applying fixes: {exc}[/red]")
            return {
                "success": False,
                "iterations": iteration,
                "fixes_applied": all_fixes_applied,
                "final_test_result": test_result,
                "stopped_reason": "fix_application_error",
            }

    final_test = test_ssh_connection_via_cli(hostname, timeout=10, parser=parser)
    return {
        "success": final_test["success"],
        "iterations": max_iterations,
        "fixes_applied": all_fixes_applied,
        "final_test_result": final_test,
        "stopped_reason": "max_iterations_reached",
    }


def generate_ssh_config(
    hostname: str, fqdn: str, username: str, port: int, auth_type: str
) -> str:
    if auth_type not in AUTH_CONFIGS:
        raise ValueError(f"Unknown auth type: {auth_type}")

    config = AUTH_CONFIGS[auth_type]
    lines = [
        f"Host {hostname}\n",
        f"  HostName {fqdn}\n",
        f"  Port {port}\n",
        f"  User {username}\n",
    ]
    lines.extend(config["config_lines"])
    lines.append("\n")
    return "".join(lines)


def detect_auth_type(host_lines: List[str]) -> str:
    config_text = "".join(host_lines)
    for key, pattern in AUTH_DETECTION_PATTERNS.items():
        if pattern.search(config_text):
            return key
    if "PubkeyAuthentication no" in config_text:
        return "keyboard-interactive"
    return "unknown"


def extract_ssh_fields(host_lines: List[str]) -> Dict[str, str]:
    config_text = "\n".join(host_lines)
    field_patterns = {
        "hostname": re.compile(r"^\s*HostName\s+(.+)$", re.MULTILINE | re.IGNORECASE),
        "port": re.compile(r"^\s*Port\s+(.+)$", re.MULTILINE | re.IGNORECASE),
        "user": re.compile(r"^\s*User\s+(.+)$", re.MULTILINE | re.IGNORECASE),
        "include": re.compile(
            r"^\s*#\s*Include:\s*(.+)$", re.MULTILINE | re.IGNORECASE
        ),
    }
    fields: Dict[str, str] = {}
    for field, pattern in field_patterns.items():
        match = pattern.search(config_text)
        fields[field] = match.group(1).strip() if match else ""
    return fields


__all__ = [
    "AUTH_CONFIGS",
    "COMPAT_CONFIGS",
    "AUTH_DETECTION_PATTERNS",
    "generate_ssh_config",
    "detect_auth_type",
    "extract_ssh_fields",
    "parse_ssh_compatibility_error",
    "test_ssh_connection_via_cli",
    "iterative_compatibility_fix",
]
