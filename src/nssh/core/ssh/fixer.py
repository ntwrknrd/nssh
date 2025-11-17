"""Authentication + compatibility helpers extracted from legacy utils."""

from __future__ import annotations

import os
import re
import subprocess
from typing import TYPE_CHECKING, Any, Dict, List

if TYPE_CHECKING:
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
        "error_pattern": r"no matching key exchange method found",
    },
    "macs": {
        "name": "Legacy MACs",
        "description": "Add legacy MAC algorithms for older SSH servers",
        "config_lines": [
            "  MACs +hmac-sha1,hmac-sha1-96,hmac-md5,hmac-md5-96\n",
        ],
        "detection_patterns": [r"MACs\s+"],
        "error_pattern": r"no matching MAC found",
    },
    "ciphers": {
        "name": "Legacy Ciphers",
        "description": "Add legacy cipher algorithms for older SSH servers",
        "config_lines": [
            "  Ciphers +aes128-cbc,3des-cbc,aes192-cbc,aes256-cbc\n",
        ],
        "detection_patterns": [r"Ciphers\s+"],
        "error_pattern": r"no matching cipher found",
    },
    "hostkey": {
        "name": "Legacy Host Key Algorithms",
        "description": "Add legacy host key algorithms for older SSH servers",
        "config_lines": [
            "  HostKeyAlgorithms +ssh-rsa,ssh-dss\n",
        ],
        "detection_patterns": [r"HostKeyAlgorithms\s+"],
        "error_pattern": r"no matching host key type found",
    },
}


def parse_ssh_compatibility_error(stderr_text: str) -> List[str]:
    needed: List[str] = []
    for compat_type, config in COMPAT_CONFIGS.items():
        pattern = str(config.get("error_pattern", ""))
        if pattern and re.search(pattern, stderr_text, re.IGNORECASE):
            needed.append(compat_type)
    return needed


def test_ssh_connection_via_wrapper(hostname: str, timeout: int = 10) -> Dict[str, Any]:
    try:
        env = os.environ.copy()
        env["NSSH_RECORD"] = "0"
        result = subprocess.run(
            ["nssh", "-V", hostname, "exit", "0"],
            capture_output=True,
            text=True,
            timeout=timeout,
            env=env,
            stdin=subprocess.DEVNULL,
        )
        return {
            "success": result.returncode == 0,
            "exit_code": result.returncode,
            "stderr": result.stderr,
            "stdout": result.stdout,
        }
    except subprocess.TimeoutExpired:
        return {
            "success": False,
            "exit_code": 124,
            "stderr": f"Connection timed out after {timeout} seconds",
            "stdout": "",
        }
    except FileNotFoundError:
        return {
            "success": False,
            "exit_code": 127,
            "stderr": "nssh command not found in PATH",
            "stdout": "",
        }
    except Exception as exc:  # pragma: no cover - defensive fallback
        return {
            "success": False,
            "exit_code": 1,
            "stderr": str(exc),
            "stdout": "",
        }


def iterative_compatibility_fix(
    parser: "SSHConfigParser",
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
        if verbose and console:
            console.print(
                f"\n[dim]Iteration {iteration}/{max_iterations}: Testing connection...[/dim]"
            )

        test_result = test_ssh_connection_via_wrapper(hostname, timeout=10)

        if test_result["success"]:
            return {
                "success": True,
                "iterations": iteration,
                "fixes_applied": all_fixes_applied,
                "final_test_result": test_result,
                "stopped_reason": "connection_succeeded",
            }

        raw_ssh_output = ""
        if "=== RAW SSH OUTPUT ===" in test_result["stderr"]:
            parts = test_result["stderr"].split("=== RAW SSH OUTPUT ===")
            if len(parts) > 1:
                raw_ssh_output = parts[1].split("=== END RAW SSH OUTPUT ===")[0].strip()

        compat_types = parse_ssh_compatibility_error(
            raw_ssh_output or test_result["stderr"]
        )

        if not compat_types:
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
                    f"[yellow]⚠ Same compatibility issues persist: {', '.join(compat_types)}[/yellow]"
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
            console.print(f"[dim]Applying fixes: {', '.join(compat_names)}[/dim]")

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

    final_test = test_ssh_connection_via_wrapper(hostname, timeout=10)
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
    "test_ssh_connection_via_wrapper",
    "iterative_compatibility_fix",
]
