"""Add command for nssh host."""

from __future__ import annotations

import os
from typing import Any, List, Optional, Set, Tuple

from nssh.cli import click
from nssh.cli.common.batch import (
    BatchResult,
    HostEntry,
    display_batch_errors,
    is_batch_file,
    parse_batch_file,
    validate_hostname,
)
from nssh.cli.common.prompt import (
    ask_text,
    ask_with_fzf,
    confirm,
    prompt_password_with_confirmation,
)
from nssh.cli.common.selectors import select_include_file
from nssh.cli.common.ssh_include import ensure_conf_d_include
from nssh.cli.common.banner import ABORT, FAIL, NOOP, OK, WARN, banner
from nssh.cli.common.ui import render_insertion_preview
from nssh.cli.common.workflows import choose_password_source
from nssh.core.env.paths import ssh_include_dir
from nssh.core.env.settings import get_config
from nssh.core.ssh.fixer import (
    generate_ssh_config,
    parse_ssh_compatibility_error,
    test_ssh_connection_via_cli,
)
from nssh.core.ssh.mutations import HostUpdateError, apply_host_update

from .compat import apply_and_display_compat_fixes
from nssh.cli.common.credentials import (
    console,
    get_manager,
    get_parser,
)


def _hostname_to_entry(hostname: str) -> HostEntry:
    """Convert a hostname string to a HostEntry (for .txt parsing)."""
    return HostEntry(hostname=hostname)


def _auto_create_contexts(
    entries: List[HostEntry], cm, parser, dry_run: bool = False
) -> Tuple[Set[str], List[str]]:
    """Auto-create missing contexts referenced in batch entries.

    Creates both the credential context and the SSH config file.

    Args:
        entries: List of HostEntry objects
        cm: CredentialManager instance
        parser: SSHConfigParser instance
        dry_run: If True, preview without making changes

    Returns:
        Tuple of (created_context_names, error_messages)
    """
    # Get existing contexts
    existing_contexts = {c.get("name") for c in cm.list_contexts()}

    # Find unique contexts that need creation
    contexts_to_create: Set[str] = set()
    for entry in entries:
        if entry.context and entry.context not in existing_contexts:
            contexts_to_create.add(entry.context)

    if not contexts_to_create:
        return set(), []

    created: Set[str] = set()
    errors: List[str] = []

    # Ensure conf.d include directive exists (needed for new config files)
    if not dry_run and contexts_to_create:
        ensure_conf_d_include(create_if_missing=True, yes=True)

    conf_d = ssh_include_dir()

    for context_name in sorted(contexts_to_create):
        # Use context name as the config file name (with _hosts suffix)
        git_include_file = f"{context_name}_hosts"
        config_file_path = conf_d / git_include_file

        if dry_run:
            console.print(
                f"[yellow]![/yellow] Would create context '{context_name}' -> '[cyan underline]{config_file_path}[/cyan underline]'"
            )
            created.add(context_name)
            continue

        try:
            # Create the SSH config file if it doesn't exist
            if not config_file_path.exists():
                conf_d.mkdir(parents=True, exist_ok=True)
                config_file_path.touch()
                console.print(
                    f"[green]+[/green] Created config file: {config_file_path}"
                )

            # Create the context in credential store
            cm.create_context(context_name, git_include_file)
            console.print(
                f"[green]+[/green] Created context '{context_name}' -> {git_include_file}"
            )
            created.add(context_name)

            # Refresh parser's include file cache
            parser._include_files = None

        except Exception as e:
            errors.append(f"Failed to create context '{context_name}': {e}")

    return created, errors


def _validate_batch_entries(
    entries: List[HostEntry],
    parser,
    cm,
    created_contexts: Optional[Set[str]] = None,
) -> Tuple[List[HostEntry], List[str], List[str]]:
    """Validate batch entries before processing.

    Args:
        entries: List of HostEntry objects to validate
        parser: SSHConfigParser instance
        cm: CredentialManager instance
        created_contexts: Set of context names that were/will be auto-created

    Returns:
        Tuple of (valid_entries, error_messages, skipped_messages)
    """
    valid = []
    errors = []
    skipped = []

    # Get current context names (may have been auto-created)
    contexts = cm.list_contexts()
    context_names = {c.get("name") for c in contexts}

    # Include contexts that were/will be auto-created (for dry-run)
    if created_contexts:
        context_names = context_names | created_contexts

    for i, entry in enumerate(entries, 1):
        # Validate hostname
        hostname_error = validate_hostname(entry.hostname)
        if hostname_error:
            errors.append(f"row {i}: {hostname_error}")
            continue

        # Check for duplicates across all config files
        is_duplicate = False
        for include_file in parser.find_include_files():
            if parser.host_exists(include_file, entry.alias):
                skipped.append(
                    f"row {i}: {entry.hostname} [italic](exists in file '{include_file.name}')[/italic]"
                )
                is_duplicate = True
                break
        if is_duplicate:
            continue

        # Validate context if specified (should exist after auto-creation)
        if entry.context and entry.context not in context_names:
            errors.append(
                f"row {i}: {entry.hostname} - context '{entry.context}' not found"
            )
            continue

        valid.append(entry)

    return valid, errors, skipped


def _display_batch_add_results(
    entries: List[HostEntry], result: BatchResult, dry_run: bool
) -> None:
    """Display batch add results grouped by context.

    Args:
        entries: List of entries that were processed
        result: BatchResult with counts
        dry_run: If True, use "would" phrasing
    """
    if not entries or result.added == 0:
        return

    symbol_style = "[dim]+[/dim]" if dry_run else "[green]+[/green]"
    verb = "Would add" if dry_run else "Adding"
    symbol = "[yellow]![/yellow]" if dry_run else "[green]+[/green]"

    # Group entries by context
    by_context: dict[Optional[str], List[HostEntry]] = {}
    for entry in entries:
        ctx = entry.context
        if ctx not in by_context:
            by_context[ctx] = []
        by_context[ctx].append(entry)

    for ctx, ctx_entries in by_context.items():
        ctx_name = ctx or "default"
        noun = "host entry" if len(ctx_entries) == 1 else "host entries"
        console.print(
            f"{symbol} {verb} {len(ctx_entries)} {noun} to '{ctx_name}' context:"
        )
        for entry in ctx_entries[:10]:
            if entry.alias != entry.hostname:
                console.print(
                    f"    {symbol_style} [dim]{entry.alias} [italic]({entry.hostname})[/italic][/dim]"
                )
            else:
                console.print(f"    {symbol_style} [dim]{entry.hostname}[/dim]")
        if len(ctx_entries) > 10:
            console.print(f"    [dim]... and {len(ctx_entries) - 10} more[/dim]")


def _batch_add(
    entries: List[HostEntry],
    parser,
    cm,
    dry_run: bool = False,
    created_contexts: Optional[Set[str]] = None,
) -> BatchResult:
    """Add multiple hosts from batch file.

    Args:
        entries: List of HostEntry objects to add
        parser: SSHConfigParser instance
        cm: CredentialManager instance
        dry_run: If True, preview without making changes
        created_contexts: Set of context names that were/will be auto-created

    Returns:
        BatchResult with counts and errors
    """
    result = BatchResult()
    config = get_config()
    created_contexts = created_contexts or set()

    for entry in entries:
        try:
            # Resolve defaults
            hostname_alias = entry.alias
            username: str = (
                entry.user
                or config.get_default_user()
                or os.getenv("USER", "admin")
                or "admin"
            )
            port = entry.port or 22

            # Find target file based on context
            context_name = entry.context or config.get_default_context()
            if context_name:
                # In dry-run mode, accept auto-created contexts without a real file
                if dry_run and context_name in created_contexts:
                    # Skip file lookup - would be created
                    target_file = None
                else:
                    # Find the config file for this context
                    contexts = cm.list_contexts()
                    target_file = None
                    for ctx in contexts:
                        if ctx.get("name") == context_name:
                            git_include_file = ctx.get("git_include_file")
                            for f in parser.find_include_files():
                                if f.name == git_include_file:
                                    target_file = f
                                    break
                            break
                    if not target_file:
                        result.failed += 1
                        result.errors.append(
                            f"{entry.hostname}: context '{context_name}' has no config file"
                        )
                        continue
            else:
                # Use first available include file
                include_files = parser.find_include_files()
                if not include_files:
                    result.failed += 1
                    result.errors.append(f"{entry.hostname}: no config files found")
                    continue
                target_file = include_files[0]

            # Check for duplicate (skip if target_file is None in dry-run)
            if target_file and parser.host_exists(target_file, hostname_alias):
                result.skipped += 1
                continue

            if dry_run:
                result.added += 1
                continue

            # Generate and write config
            config_block = generate_ssh_config(
                hostname_alias, entry.hostname, username, port, "password"
            )

            header_lines, hosts = parser.parse_ssh_config(target_file)
            insert_index = parser.find_insertion_index(hosts, hostname_alias)

            lines = config_block.split("\n")
            config_lines = [
                (line + "\n") if idx < len(lines) - 1 or line else "\n"
                for idx, line in enumerate(lines)
                if line or idx == len(lines) - 1
            ]

            hosts.insert(insert_index, (hostname_alias, config_lines))
            parser.write_ssh_config(target_file, header_lines, hosts)

            # Store password if provided
            if entry.password:
                cm.add_host_credential(hostname_alias, username, entry.password)

            result.added += 1

        except Exception as e:
            result.failed += 1
            result.errors.append(f"{entry.hostname}: {str(e)}")

    # Rebuild index after batch operation
    if not dry_run:
        parser.rebuild_index()

    return result


def _interactive_add(
    ctx: click.Context,
    fqdn: Optional[str],
    yes: bool,
    dry_run: bool,
    force: bool,
    set_outcome,
) -> None:
    """Interactive flow for adding a single host."""
    cm = get_manager(ctx)
    parser = get_parser(ctx)
    config = get_config()

    # FQDN (re-prompt until valid)
    final_fqdn = fqdn
    while True:
        if not final_fqdn:
            final_fqdn = ask_text("FQDN")

        if final_fqdn and "." in final_fqdn:
            break

        console.print("[red]Invalid FQDN (must contain at least one '.')[/red]")
        final_fqdn = None  # Reset to re-prompt

    # Alias (derived from FQDN)
    default_alias = final_fqdn.split(".")[0]
    if yes:
        final_hostname = default_alias
    else:
        final_hostname = ask_text("Alias", default=default_alias)

    # Port
    if yes:
        final_port = 22
    else:
        final_port_input = ask_text("Port", default="22")
        try:
            final_port = int(final_port_input)
        except ValueError:
            console.print("[red]Error: Invalid port number[/red]")
            raise SystemExit(1)

    # Context selection (with domain pattern matching)
    matched_context_name = None
    fqdn_lower = final_fqdn.lower()
    for context_item in cm.list_contexts():
        domain = context_item.get("domain", "")
        if domain:
            domain_lower = domain.lower()
            if fqdn_lower == domain_lower or fqdn_lower.endswith(f".{domain_lower}"):
                matched_context_name = context_item.get("name")
                break

    # Build context choices for fzf
    contexts = cm.list_contexts()
    context_choices: list[str] = [
        name for c in contexts if (name := c.get("name")) is not None
    ]

    # Determine default context
    default_context: str | None
    if matched_context_name:
        default_context = f"domain matched: [cyan]'{matched_context_name}'[/cyan]"
    else:
        default_context = config.get_default_context()

    # Prompt for context (Tab to browse)
    if yes and matched_context_name:
        selected_context: str | None = matched_context_name
    elif context_choices:
        selected_context = ask_with_fzf(
            "Context",
            default=default_context,
            fzf_choices=context_choices,
            fzf_prompt="Select context:",
        )
        # Strip "domain matched: ..." wrapper if present
        if selected_context and "domain matched:" in selected_context:
            selected_context = matched_context_name
    else:
        selected_context = None

    # Resolve context to target file
    if selected_context:
        target_selection = select_include_file(
            parser, selected_context, "Select config file:"
        )
    else:
        # No context, use first available include file
        include_files = parser.find_include_files()
        if not include_files:
            console.print("[red]Error: No config files found[/red]")
            raise SystemExit(1)
        target_selection = include_files[0]

    if isinstance(target_selection, list):
        console.print(
            "[red]Error: Multiple files selected; only one config file can be targeted here[/red]"
        )
        raise SystemExit(1)
    target_file = target_selection

    # Check for duplicates
    if parser.host_exists(target_file, final_hostname):
        console.print(
            f"[red]Error: Host '{final_hostname}' already exists in {target_file.name}[/red]"
        )
        raise SystemExit(1)

    # Username
    default_user = config.get_default_user()
    if not default_user:
        git_include_file = target_file.name
        file_context: dict[str, Any] | None = cm.get_context(git_include_file)
        if file_context and file_context.get("credential"):
            default_user = file_context["credential"]["username"]
        else:
            default_user = os.getenv("USER", "admin")

    if yes:
        username = default_user
    else:
        username = ask_text("Username", default=default_user)

    # Authentication
    git_include_file = target_file.name
    pwd_context: dict[str, Any] | None = cm.get_context(git_include_file)
    has_context_creds = bool(pwd_context and pwd_context.get("credential"))
    auth_type = "password"

    password_choice = choose_password_source(
        context_name=pwd_context["name"] if pwd_context else None,
        has_context_credentials=has_context_creds,
        skip_prompt=yes,
    )

    if password_choice == "custom":
        try:
            pwd = prompt_password_with_confirmation("Password")
        except ValueError as e:
            console.print(f"[red]{e}[/red]")
            raise SystemExit(1)

        if not dry_run:
            try:
                cm.add_host_credential(final_hostname, username, pwd)
            except Exception as e:
                console.print(f"[red]Error storing password: {e}[/red]")
                raise SystemExit(1)

    # Generate SSH config block
    config_block = generate_ssh_config(
        final_hostname, final_fqdn, username, final_port, auth_type
    )

    # Parse target file for insertion preview
    header_lines, hosts = parser.parse_ssh_config(target_file)
    insert_index = parser.find_insertion_index(hosts, final_hostname)

    # Get surrounding host config lines for preview
    before_lines = None
    after_lines = None
    if insert_index > 0:
        before_lines = hosts[insert_index - 1][1]
    if insert_index < len(hosts):
        after_lines = hosts[insert_index][1]

    # Preview
    console.print()
    render_insertion_preview(
        config_block,
        before_lines,
        after_lines,
        str(target_file),
    )

    # Confirm
    if dry_run:
        console.print("\n[dim]Dry-run: no changes made[/dim]")
        set_outcome(NOOP)
        return

    if not yes:
        console.print()
        if not confirm("Add host?"):
            console.print("[yellow]Cancelled[/yellow]")
            set_outcome(ABORT)
            raise SystemExit(0)

    # Write config
    try:
        parser.create_backup(target_file)

        lines = config_block.split("\n")
        config_lines = [
            (line + "\n") if i < len(lines) - 1 or line else "\n"
            for i, line in enumerate(lines)
            if line or i == len(lines) - 1
        ]

        hosts.insert(insert_index, (final_hostname, config_lines))
        parser.write_ssh_config(target_file, header_lines, hosts)
        parser.rebuild_index()

        # Test connection (unless --force)
        test_connection = config.host_add.test_connection
        if matched_context_name:
            ctx_config = config.contexts.get(matched_context_name)
            if ctx_config and ctx_config.test_connection is not None:
                test_connection = ctx_config.test_connection

        if not force and test_connection:
            console.print("[dim]Testing connection...[/dim]")
            test_result = test_ssh_connection_via_cli(
                final_hostname, timeout=10, parser=parser
            )

            if test_result["success"]:
                detected_method = test_result.get("auth_method")
                if (
                    auth_type == "password"
                    and detected_method == "keyboard-interactive"
                ):
                    try:
                        apply_host_update(
                            parser,
                            final_hostname,
                            auth_type="keyboard-interactive",
                            create_backup=False,
                        )
                    except HostUpdateError:
                        pass
            else:
                # Try to fix compatibility issues
                fix_succeeded = _handle_test_failure(
                    test_result, final_hostname, parser, auth_type
                )
                if not fix_succeeded:
                    # Remove the host since test failed
                    console.print()
                    console.print("[red]✗[/red] Connection test failed - removing host")
                    header_lines, hosts = parser.parse_ssh_config(target_file)
                    hosts = [(h, lines) for h, lines in hosts if h != final_hostname]
                    parser.write_ssh_config(target_file, header_lines, hosts)
                    parser.rebuild_index()
                    # Clean up any stored credentials
                    try:
                        cm.delete_host_all_credentials(final_hostname)
                    except Exception:
                        pass
                    # Save debug file with restrictive permissions
                    import stat
                    import tempfile
                    from pathlib import Path

                    debug_dir = Path("/tmp/nssh")
                    debug_dir.mkdir(exist_ok=True)
                    with tempfile.NamedTemporaryFile(
                        mode="w",
                        suffix=".txt",
                        prefix="nssh-debug-",
                        dir=str(debug_dir),
                        delete=False,
                    ) as handle:
                        handle.write(f"Hostname: {final_hostname}\n")
                        handle.write(f"Exit code: {test_result['exit_code']}\n\n")
                        handle.write("STDERR:\n")
                        handle.write(test_result.get("stderr", "") or "")
                        handle.write("\n\nSTDOUT:\n")
                        handle.write(test_result.get("stdout", "") or "")
                        debug_file = handle.name
                    os.chmod(debug_file, stat.S_IRUSR | stat.S_IWUSR)
                    console.print(f"[yellow]![/yellow] Debug info: {debug_file}")
                    console.print(
                        "[yellow]![/yellow] Use --force to add without testing"
                    )
                    set_outcome(FAIL)
                    raise SystemExit(1)

        # Final status
        console.print()
        console.print(
            f"[green]+[/green] Host '{final_hostname}' added to {target_file.name}"
        )

    except Exception as e:
        console.print(f"[red]Error: {e}[/red]")
        import traceback

        traceback.print_exc()
        raise SystemExit(1)


def _handle_test_failure(
    test_result: dict, hostname: str, parser, auth_type: str
) -> bool:
    """Handle connection test failure with potential auto-fix.

    Returns:
        True if fix was applied successfully, False otherwise.
    """
    # Only attempt auto-fix for SSH protocol errors (exit code 255)
    if test_result["exit_code"] == 255:
        # Extract raw SSH output
        raw_ssh_output = ""
        stderr = test_result["stderr"]
        if "=== RAW SSH OUTPUT ===" in stderr:
            parts = stderr.split("=== RAW SSH OUTPUT ===")
            if len(parts) > 1:
                raw_part = parts[1].split("=== END RAW OUTPUT ===")[0]
                raw_ssh_output = raw_part.strip()

        # Detect compatibility issues
        compat_types = parse_ssh_compatibility_error(
            raw_ssh_output or test_result["stderr"] or test_result["stdout"]
        )

        if compat_types:
            console.print("[dim]Legacy SSH compatibility issue detected[/dim]")
            if confirm("Apply compatibility fix?", default=True):
                result = apply_and_display_compat_fixes(
                    parser,
                    hostname,
                    max_iterations=5,
                    show_header=False,
                )
                return result.get("success", False)

    return False


@click.command(short_help="Add host(s)")
@click.argument("host_or_file", metavar="FQDN|FILE", required=False, default=None)
@click.option("-y", "--yes", is_flag=True, default=False, help="Accept defaults")
@click.option("--dry-run", is_flag=True, default=False, help="Preview only")
@click.option("-f", "--force", is_flag=True, default=False, help="Skip connection test")
@click.pass_context
def add_command(
    ctx: click.Context,
    host_or_file: Optional[str],
    yes: bool,
    dry_run: bool,
    force: bool,
) -> None:
    """Add SSH host(s) to configuration.

    Single-host mode (interactive):
        nssh host add server.example.com
        nssh host add server.example.com -y  # use defaults

    Batch mode (from file):
        nssh host add ./hosts.txt   # one hostname per line
        nssh host add ./hosts.csv   # CSV with headers
        nssh host add ./hosts.json  # JSON array
    """
    # Detect mode
    if host_or_file and is_batch_file(host_or_file):
        # Batch mode
        with banner("BATCH ADD HOSTS", OK) as set_outcome:
            _batch_add_mode(ctx, host_or_file, dry_run, set_outcome)
    else:
        # Interactive/single-host mode
        with banner("ADD SSH HOST", OK) as set_outcome:
            _interactive_add(ctx, host_or_file, yes, dry_run, force, set_outcome)


def _batch_add_mode(ctx, host_or_file, dry_run, set_outcome) -> None:
    """Batch add mode - add multiple hosts from file."""
    cm = get_manager(ctx)
    parser = get_parser(ctx)

    try:
        entries = parse_batch_file(
            host_or_file, HostEntry, txt_to_entry=_hostname_to_entry
        )
    except FileNotFoundError as e:
        console.print(f"[red]Error: {e}[/red]")
        raise SystemExit(1)
    except ValueError as e:
        console.print(f"[red]Error parsing file: {e}[/red]")
        raise SystemExit(1)

    console.print(
        f"\n[green]\u2713[/green] Loaded {len(entries)} entries from {host_or_file}"
    )

    # Auto-create missing contexts
    created_contexts, context_errors = _auto_create_contexts(
        entries, cm, parser, dry_run=dry_run
    )
    if context_errors:
        console.print("\n[yellow]Context creation errors:[/yellow]")
        for error in context_errors:
            console.print(f"  [red]{error}[/red]")

    # Validate entries (after context auto-creation)
    valid_entries, validation_errors, skipped_entries = _validate_batch_entries(
        entries, parser, cm, created_contexts=created_contexts
    )

    # Display actual validation errors
    if validation_errors:
        console.print(f"[red]![/red] {len(validation_errors)} validation error(s)")
        for error in validation_errors[:5]:
            console.print(f"  [dim]{error}[/dim]")
        if len(validation_errors) > 5:
            console.print(f"  [dim]... and {len(validation_errors) - 5} more[/dim]")

    # Display skipped entries (already exist - informational)
    if skipped_entries:
        console.print(
            f"[yellow]![/yellow] {len(skipped_entries)} skipped (already exist)"
        )
        for skip in skipped_entries[:5]:
            console.print(f"  [dim]{skip}[/dim]")
        if len(skipped_entries) > 5:
            console.print(f"  [dim]... and {len(skipped_entries) - 5} more[/dim]")

    if not valid_entries:
        # No hosts to add, but context may have been created
        console.print()
        if skipped_entries and not validation_errors:
            # All entries already exist - idempotent success
            console.print("[dim]No changes required[/dim]")
            set_outcome(NOOP)
        elif created_contexts and not dry_run:
            # Context was created successfully but no hosts added
            set_outcome(WARN)
        elif created_contexts and dry_run:
            set_outcome(NOOP)
        else:
            console.print("[red]No valid entries to process[/red]")
            set_outcome(FAIL)
            raise SystemExit(1)
        return

    # Process entries
    result = _batch_add(
        valid_entries,
        parser,
        cm,
        dry_run=dry_run,
        created_contexts=created_contexts,
    )

    # Display results grouped by context
    _display_batch_add_results(valid_entries, result, dry_run)
    display_batch_errors(result)

    # Completion message
    if dry_run:
        set_outcome(NOOP)
    elif result.has_failures():
        set_outcome(FAIL)
        raise SystemExit(1)
    # else: default "Hosts imported" footer will be used
