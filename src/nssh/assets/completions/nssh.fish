# Fish shell completion for nssh
# Hybrid: Typer for subcommands + manual hostname completion for bare connect

# Typer-generated completion (handles all subcommands)
complete --command nssh --no-files --arguments "(env _NSSH_COMPLETE=complete_fish _TYPER_COMPLETE_FISH_ACTION=get-args _TYPER_COMPLETE_ARGS=(commandline -cp) nssh)" --condition "env _NSSH_COMPLETE=complete_fish _TYPER_COMPLETE_FISH_ACTION=is-args _TYPER_COMPLETE_ARGS=(commandline -cp) nssh"

# Helper to check if we're at top level (no subcommand chosen yet)
function __nssh_at_toplevel
    set -l tokens (commandline -opc)
    test (count $tokens) -eq 1
end

# Hostname completion for bare connect path: nssh <hostname>
# This completes from ~/.ssh/.nssh_host_index
complete -c nssh -n "__nssh_at_toplevel" -f -a "(cut -d'|' -f1 ~/.ssh/.nssh_host_index 2>/dev/null)"
