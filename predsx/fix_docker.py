import os
import glob

def fix_dockerfile(path):
    with open(path, 'r') as f:
        content = f.read()

    # Revert all our messy changes by generating the correct block
    lines = content.split('\n')
    new_lines = []
    
    in_copy_block = False
    for line in lines:
        # Strip all existing COPY commands for local modules to avoid duplicates
        if line.startswith('COPY libs/') or line.startswith('COPY go.work') or line.startswith('COPY cmd/'):
            continue
            
        # Strip the replace hack we added earlier
        if 'go mod edit -replace' in line:
            line = 'RUN go mod download'
            
        if line.startswith('COPY go.mod') or line.startswith('COPY go.sum'):
            continue
            
        new_lines.append(line)
        
    # Find where to insert our standard COPY block (right after ENV GOFLAGS)
    insert_idx = 0
    for i, line in enumerate(new_lines):
        if line.startswith('WORKDIR /app'):
            insert_idx = i + 1
            break
            
    # Standard correct dependencies for all PredSX Dockerfiles
    deps = [
        "",
        "COPY go.work go.work.sum ./",
        "COPY libs/ ./libs/",
        "COPY cmd/predsx/ ./cmd/predsx/",
    ]
    
    # We copy everything, so let's just use GOWORK=on
    for i, line in enumerate(new_lines):
        if line.startswith('ENV GOWORK=off'):
            new_lines[i] = 'ENV GOWORK=on'
            
    final_lines = new_lines[:insert_idx] + deps + new_lines[insert_idx:]
    
    with open(path, 'w') as f:
        f.write('\n'.join(final_lines))

for f in glob.glob('services/*/Dockerfile'):
    fix_dockerfile(f)
