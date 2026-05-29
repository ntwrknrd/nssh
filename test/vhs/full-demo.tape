# Demo: nssh full workflow
# Shows initialization, inventory/credential management, and SSH connection with password injection

Set Width 1200
Set Height 900
Set Margin 20
Set MarginFill "#674EFF"
Set WindowBar Colorful
Set WindowBarSize 40
Set BorderRadius 8
Set Padding 10
Set Theme "iTerm2 Pastel Dark Background"
Set Framerate 60
Set TypingSpeed 60ms
Set Shell "bash"
Set FontSize 20
Set FontFamily "JetBrains Mono"
# Set LoopOffset 5

# ============================================================================
# Section 1: Self Init
# ============================================================================

Sleep 500ms

# Start the interactive init
Type "nssh self init"
Sleep 500ms
Enter

# Wait for the user to review
Sleep 2s

# Confirm software security mode
Enter
Sleep 500ms

# Enter passphrase
Type "thisisatestpass"
Enter
Sleep 500ms

# Confirm passphrase
Type "thisisatestpass"
Enter
Sleep 500ms

# Skip dependency installs
Right
Sleep 500ms
Enter
Sleep 500ms
Right
Sleep 500ms
Enter
Sleep 500ms
Right
Sleep 500ms
Enter
Sleep 500ms

# Unlock the vault for subsequent commands
Sleep 200ms
Type "nssh unlock"
Enter

# Enter passphrase
Sleep 200ms
Type "thisisatestpass"
Enter

# Pause for user to review
Sleep 10s
Ctrl+L

# ============================================================================
# Section 2: Group Add & List
# ============================================================================

# Create an inventory group
Type "nssh inv set --group test"
Sleep 300ms
Enter

# Show the created group
Type "nssh inv list --groups"
Sleep 500ms
Enter

# Pause for user to review
Sleep 10s
Ctrl+L

# ============================================================================
# Section 4: Inventory Host Add
# ============================================================================

# Create a local inventory host
Type "nssh inv set ssh-pwd.demo.local --group test --user testuser"
Sleep 300ms
Enter

# Save password as a host credential
Type "nssh cred set --host ssh-pwd.demo.local --username testuser"
Sleep 300ms
Enter
Sleep 500ms
Type "testpass"
Enter
Sleep 500ms
Type "testpass"
Enter
Sleep 1s

# Show the created host
Type "nssh inv list"
Sleep 500ms
Enter

# Pause for user to review
Sleep 10s
Ctrl+L

# ============================================================================
# Section 6: Connect
# ============================================================================

# Connect to the test host (password will be auto-injected)
Type "nssh ssh-pwd"
Sleep 300ms
Enter

# Wait for connection and password injection
Sleep 2s

# Run some commands to show we're connected
Type "whoami"
Enter
Sleep 2s

Type "hostname"
Enter
Sleep 2s

Type@20ms "echo 'Connected via nssh with auto password injection!'"
Enter
Sleep 2s

# Exit the session
Type "exit"
Enter

# Let viewer see the disconnect
Sleep 10s
