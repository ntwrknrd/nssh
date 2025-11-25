"""Input validation and sanitization for SSH and SCP operations."""

from __future__ import annotations

from typing import Sequence

# Whitelist of safe scp command-line options
SAFE_SCP_OPTIONS = {
    "-r",  # Recursive copy
    "-p",  # Preserve modification times and modes
    "-q",  # Quiet mode
    "-v",  # Verbose mode
    "-C",  # Enable compression
    "-P",  # Port (takes argument)
    "-c",  # Cipher (takes argument)
    "-i",  # Identity file (takes argument)
    "-F",  # Config file (takes argument)
    "-l",  # Limit bandwidth (takes argument)
}

# Options that take an additional argument
OPTIONS_WITH_ARGS = {"-P", "-c", "-i", "-F", "-l"}

# Dangerous SSH options that must be blocked
DANGEROUS_SSH_OPTIONS = {
    "proxycommand",
    "localcommand",
    "remotecommand",
    "permitlocalcommand",
}


def validate_scp_args(args: Sequence[str]) -> list[str]:
    """Validate and sanitize scp command-line arguments.

    This function implements a whitelist approach to prevent command injection
    attacks via scp arguments. It blocks dangerous options like -S (program),
    -o ProxyCommand, and other vectors that could allow arbitrary command execution.

    Args:
        args: Sequence of scp command-line arguments to validate

    Returns:
        List of validated arguments safe for use with scp

    Raises:
        ValueError: If any argument is disallowed or contains dangerous content

    Example:
        >>> validate_scp_args(["-r", "-p"])
        ['-r', '-p']
        >>> validate_scp_args(["-S", "/usr/bin/evil"])
        ValueError: Disallowed scp option: -S
    """
    validated = []
    i = 0

    while i < len(args):
        arg = args[i]

        # Block -S (allows arbitrary program execution)
        if arg == "-S":
            raise ValueError(
                f"Disallowed scp option: {arg} (can execute arbitrary programs)"
            )

        # Block standalone -o (SSH options that can inject commands)
        if arg == "-o":
            if i + 1 >= len(args):
                raise ValueError("Option -o requires an argument")
            option = args[i + 1]
            # Check for dangerous SSH options
            option_lower = option.lower()
            for dangerous in DANGEROUS_SSH_OPTIONS:
                if dangerous in option_lower:
                    raise ValueError(
                        f"Disallowed SSH option: {option} "
                        f"(can execute arbitrary commands)"
                    )

        # Check for -o<option> format (no space)
        if arg.startswith("-o") and len(arg) > 2:
            option = arg[2:]
            option_lower = option.lower()
            for dangerous in DANGEROUS_SSH_OPTIONS:
                if dangerous in option_lower:
                    raise ValueError(
                        f"Disallowed SSH option: {option} "
                        f"(can execute arbitrary commands)"
                    )

        # Whitelist safe options
        if arg in SAFE_SCP_OPTIONS:
            validated.append(arg)
            # If option takes argument, validate next item exists and include it
            if arg in OPTIONS_WITH_ARGS:
                if i + 1 >= len(args):
                    raise ValueError(f"Option {arg} requires an argument")
                i += 1
                validated.append(args[i])
        elif arg.startswith("-"):
            # Unknown option - reject for safety
            raise ValueError(
                f"Unknown or disallowed scp option: {arg}. "
                f"Allowed options: {', '.join(sorted(SAFE_SCP_OPTIONS))}"
            )
        else:
            # Non-option arguments should not appear in scp_args
            # (source/dest are handled separately)
            raise ValueError(
                f"Unexpected non-option argument: {arg}. "
                f"Source and destination paths should not be in scp_args."
            )

        i += 1

    return validated


def validate_remote_path(path: str) -> str:
    """Validate remote path specification for SCP operations.

    This function checks for common attack vectors in file paths including:
    - Null bytes (can truncate paths in C code)
    - Paths starting with '-' (can be interpreted as options)
    - Excessive parent directory references (path traversal attempts)

    Args:
        path: The path string to validate (can be local or remote)

    Returns:
        The validated path string (unchanged if valid)

    Raises:
        ValueError: If path contains suspicious or dangerous content

    Example:
        >>> validate_remote_path("~/file.txt")
        '~/file.txt'
        >>> validate_remote_path("-evil.txt")
        ValueError: Path cannot start with '-'
    """
    # Check for null bytes (can cause truncation in C code)
    if "\0" in path:
        raise ValueError("Null byte detected in path")

    # Prevent paths that look like options
    if path.startswith("-"):
        raise ValueError(
            "Path cannot start with '-' (could be interpreted as an option)"
        )

    # Detect excessive path traversal attempts
    # More than 3 '../' is suspicious for legitimate file operations
    if path.count("..") > 3:
        raise ValueError(
            "Excessive parent directory references detected "
            "(possible path traversal attempt)"
        )

    return path


# Whitelist of safe SSH command-line options based on OpenSSH man page
# Categories: connection, authentication, config, forwarding, multiplexing,
# encryption, logging, terminal, session, X11, compression, tunneling, misc
SAFE_SSH_OPTIONS = {
    # Connection options
    "-4",  # Force IPv4
    "-6",  # Force IPv6
    "-p",  # Port (takes argument)
    "-B",  # Bind interface (takes argument)
    "-b",  # Bind address (takes argument)
    # Authentication options
    "-i",  # Identity file (takes argument)
    "-I",  # PKCS11 provider (takes argument)
    "-K",  # Enable GSSAPI credential forwarding
    "-k",  # Disable GSSAPI credential forwarding
    "-A",  # Enable agent forwarding
    "-a",  # Disable agent forwarding
    # Config options
    "-F",  # Config file (takes argument)
    # Forwarding options
    "-L",  # Local port forward (takes argument)
    "-R",  # Remote port forward (takes argument)
    "-D",  # Dynamic port forward (takes argument)
    "-g",  # Allow remote hosts to connect to forwarded ports
    "-J",  # Jump host (takes argument) - safer than ProxyCommand
    "-W",  # Forward stdin/stdout (takes argument)
    # Multiplexing options
    "-M",  # Master mode for connection sharing
    "-O",  # Control command (takes argument)
    "-S",  # Control socket path (takes argument)
    # Encryption/MAC options
    "-c",  # Cipher spec (takes argument)
    "-m",  # MAC spec (takes argument)
    # Logging/debug options
    "-E",  # Log file (takes argument)
    "-v",  # Verbose (can be repeated)
    "-q",  # Quiet mode
    "-y",  # Send logs via syslog
    # Terminal options
    "-T",  # Disable pseudo-terminal
    "-t",  # Force pseudo-terminal (can be repeated)
    "-e",  # Escape character (takes argument)
    # Session control
    "-N",  # No remote command execution
    "-n",  # Redirect stdin from /dev/null
    "-f",  # Background before command execution
    "-s",  # Request subsystem invocation
    # X11 forwarding
    "-X",  # Enable X11 forwarding
    "-x",  # Disable X11 forwarding
    "-Y",  # Enable trusted X11 forwarding
    # Compression
    "-C",  # Enable compression
    # Tunneling
    "-w",  # Tunnel device forwarding (takes argument)
    # Query/info options
    "-Q",  # Query algorithms (takes argument)
    "-V",  # Display version
    "-G",  # Print configuration and exit
    # Tag options
    "-P",  # Tag name (takes argument)
    # Generic SSH config option (with validation)
    "-o",  # SSH config option (takes argument, validated separately)
}

# SSH options that take an additional argument
SSH_OPTIONS_WITH_ARGS = {
    "-p",
    "-B",
    "-b",
    "-i",
    "-I",
    "-F",
    "-L",
    "-R",
    "-D",
    "-J",
    "-W",
    "-O",
    "-S",
    "-c",
    "-m",
    "-E",
    "-e",
    "-w",
    "-Q",
    "-P",
    "-o",
}


def validate_ssh_args(args: Sequence[str]) -> list[str]:
    """Validate and sanitize SSH command-line arguments.

    This function implements a whitelist approach to prevent command injection
    attacks via SSH arguments. It allows all legitimate SSH options while
    blocking dangerous -o options like ProxyCommand and LocalCommand.

    Args:
        args: Sequence of SSH command-line arguments to validate

    Returns:
        List of validated arguments safe for use with SSH

    Raises:
        ValueError: If any argument is disallowed or contains dangerous content

    Example:
        >>> validate_ssh_args(["-v", "-p", "2222"])
        ['-v', '-p', '2222']
        >>> validate_ssh_args(["-o", "ProxyCommand=/bin/evil"])
        ValueError: Disallowed SSH option: ProxyCommand=...
    """
    validated = []
    i = 0

    while i < len(args):
        arg = args[i]

        # Handle -o options specially (check for dangerous sub-options)
        if arg == "-o":
            if i + 1 >= len(args):
                raise ValueError("Option -o requires an argument")
            option = args[i + 1]
            # Check for dangerous SSH config options
            option_lower = option.lower()
            for dangerous in DANGEROUS_SSH_OPTIONS:
                if dangerous in option_lower:
                    raise ValueError(
                        f"Disallowed SSH option: {option} "
                        f"(can execute arbitrary commands)"
                    )
            validated.append(arg)
            validated.append(option)
            i += 2
            continue

        # Check for -o<option> format (no space)
        if arg.startswith("-o") and len(arg) > 2:
            option = arg[2:]
            option_lower = option.lower()
            for dangerous in DANGEROUS_SSH_OPTIONS:
                if dangerous in option_lower:
                    raise ValueError(
                        f"Disallowed SSH option: {option} "
                        f"(can execute arbitrary commands)"
                    )
            validated.append(arg)
            i += 1
            continue

        # Handle -- separator (marks end of options, start of remote command)
        if arg == "--":
            # Everything from here onward is the remote command
            # Pass through the rest of the arguments without validation
            validated.extend(args[i:])
            break
        # Whitelist safe options
        elif arg in SAFE_SSH_OPTIONS:
            validated.append(arg)
            # If option takes argument, validate next item exists and include it
            if arg in SSH_OPTIONS_WITH_ARGS and arg != "-o":  # -o handled above
                if i + 1 >= len(args):
                    raise ValueError(f"Option {arg} requires an argument")
                i += 1
                validated.append(args[i])
        elif arg.startswith("-"):
            # Unknown option - reject for safety
            raise ValueError(
                f"Unknown or disallowed SSH option: {arg}. "
                f"See 'man ssh' for allowed options."
            )
        else:
            # Non-option arguments (hostname, command, etc.) - pass through
            # These are handled by connect.py and should be allowed here
            validated.append(arg)

        i += 1

    return validated
