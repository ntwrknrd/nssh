#!/usr/bin/env bash
# One-time migration script to generate .index.json files for existing cast files.
# This fixes duration display for files recorded before index writing was implemented.

set -uo pipefail

CASTS_DIR="${1:-$HOME/.local/state/nssh/casts}"

if [[ ! -d "$CASTS_DIR" ]]; then
    echo "Usage: $0 [casts_directory]"
    echo "Default: ~/.local/state/nssh/casts"
    exit 1
fi

echo "Migrating cast files in: $CASTS_DIR"
echo ""

migrated=0
skipped=0
failed=0

while IFS= read -r -d '' cast_file; do
    index_file="${cast_file%.cast}.index.json"
    
    # Skip if index already exists
    if [[ -f "$index_file" ]]; then
        ((skipped++))
        continue
    fi
    
    # Extract metadata and calculate duration
    result=$(python3 << PYTHON
import json
import os
import sys
from datetime import datetime

cast_path = "$cast_file"

try:
    with open(cast_path, 'r') as f:
        # Read header
        header = json.loads(f.readline())
        
        # Get start time from header
        started_at = datetime.fromtimestamp(header.get('timestamp', 0), tz=None)
        
        # Extract hostname from title (format: "nssh:hostname")
        title = header.get('title', '')
        if title.startswith('nssh:'):
            host = title[5:]
        else:
            # Fallback: extract from path
            parts = cast_path.split(os.sep)
            host = parts[-3] if len(parts) >= 3 else 'unknown'
        
        # Sum all relative timestamps (v3 format)
        total_seconds = 0.0
        for line in f:
            line = line.strip()
            if not line or not line.startswith('['):
                continue
            try:
                event = json.loads(line)
                if len(event) >= 1 and isinstance(event[0], (int, float)):
                    total_seconds += event[0]
            except:
                continue
        
        # Calculate finished time
        from datetime import timedelta
        finished_at = started_at + timedelta(seconds=total_seconds)
        
        # Build index payload
        argv = header.get('command', '').split()
        
        # Extract session label from filename
        basename = os.path.basename(cast_path)
        import re
        match = re.search(r'session-(\d+)\.cast$', basename)
        session_label = f"session-{match.group(1)}" if match else ""
        
        index = {
            "host": host,
            "cast": cast_path,
            "sessions": [{
                "host": host,
                "started_at": started_at.strftime("%Y-%m-%dT%H:%M:%SZ"),
                "finished_at": finished_at.strftime("%Y-%m-%dT%H:%M:%SZ"),
                "exit_code": 0,
                "auth": "",
                "argv": argv,
                "session": session_label
            }]
        }
        
        print(json.dumps(index))
        
except Exception as e:
    print(f"ERROR: {e}", file=sys.stderr)
    sys.exit(1)
PYTHON
)
    
    if [[ $? -ne 0 ]]; then
        echo "FAILED: $cast_file"
        ((failed++))
        continue
    fi
    
    # Write index file
    echo "$result" | python3 -c "import json,sys; print(json.dumps(json.load(sys.stdin), indent=2))" > "$index_file"
    
    # Show what we did
    duration=$(echo "$result" | python3 -c "
import json,sys
from datetime import datetime
d = json.load(sys.stdin)
s = d['sessions'][0]
start = datetime.fromisoformat(s['started_at'].rstrip('Z'))
end = datetime.fromisoformat(s['finished_at'].rstrip('Z'))
delta = end - start
hours, rem = divmod(int(delta.total_seconds()), 3600)
mins, secs = divmod(rem, 60)
if hours:
    print(f'{hours:02d}:{mins:02d}:{secs:02d}')
else:
    print(f'{mins:02d}:{secs:02d}')
")
    
    echo "MIGRATED: $(basename "$(dirname "$(dirname "$cast_file")")")/$(basename "$(dirname "$cast_file")")/$(basename "$cast_file") → $duration"
    ((migrated++))
    
done < <(find "$CASTS_DIR" -name "*.cast" -print0)

echo ""
echo "Done: $migrated migrated, $skipped skipped (already have index), $failed failed"
