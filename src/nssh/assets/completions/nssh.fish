# Fish shell completion for nssh
# Click-generated completion (handles all subcommands)
function _nssh_completion
    set -l response (env _NSSH_COMPLETE=fish_complete COMP_WORDS=(commandline -cp) COMP_CWORD=(commandline -t) nssh)

    for completion in $response
        set -l metadata (string split "," $completion)

        if test $metadata[1] = "dir"
            __fish_complete_directories $metadata[2]
        else if test $metadata[1] = "file"
            __fish_complete_path $metadata[2]
        else if test $metadata[1] = "plain"
            echo $metadata[2]
        end
    end
end

complete --no-files --command nssh --arguments "(_nssh_completion)"

# Helper to check if we're at top level (no subcommand chosen yet)
function __nssh_at_toplevel
    set -l tokens (commandline -opc)
    test (count $tokens) -eq 1
end

function __nssh_host_completion
    set -l index_path $NSSH_HOST_INDEX
    if test -z "$index_path"
        set index_path ~/.local/state/nssh/host_index
    end
    if test -r $index_path
        cut -d'|' -f1 $index_path 2>/dev/null
    end
end

# Hostname completion for bare connect path: nssh <hostname>
complete -c nssh -n "__nssh_at_toplevel" -f -a "(__nssh_host_completion)"
