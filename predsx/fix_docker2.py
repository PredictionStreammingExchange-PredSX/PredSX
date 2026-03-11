import os
import glob

def fix_dockerfile(path):
    with open(path, 'r') as f:
        content = f.read()

    lines = content.split('\n')
    new_lines = []
    
    in_copy_block = False
    for line in lines:
        if line.startswith('COPY libs/') or line.startswith('COPY go.work') or line.startswith('COPY cmd/'):
            continue
        if 'go mod edit -replace' in line:
            continue
        if line.startswith('COPY go.mod') or line.startswith('COPY go.sum'):
            continue
        new_lines.append(line)
        
    insert_idx = 0
    for i, line in enumerate(new_lines):
        if line.startswith('WORKDIR /app'):
            insert_idx = i + 1
            break
            
    # Extract service name
    service_name = path.split('\\')[-2] if '\\' in path else path.split('/')[-2]
    
    replace_cmd = "RUN go mod edit "
    replaces = [
        "clickhouse-client", "config", "crypto", "kafka-client", "logger",
        "postgres-client", "redis-client", "retry-utils", "schemas", "service", "websocket-client"
    ]
    for lib in replaces:
        replace_cmd += f"-replace github.com/predsx/predsx/libs/{lib}=../../libs/{lib} "
    
    deps = [
        "",
        "COPY go.mod go.sum ./",
        "COPY libs/ ./libs/",
        "COPY cmd/predsx/ ./cmd/predsx/",
        f"COPY services/{service_name}/go.mod services/{service_name}/go.sum ./services/{service_name}/",
        f"WORKDIR /app/services/{service_name}",
        replace_cmd,
        "RUN go mod download",
        "WORKDIR /app",
        f"COPY services/{service_name}/ ./services/{service_name}/",
    ]
    
    for i, line in enumerate(new_lines):
        if line.startswith('ENV GOWORK=on'):
            new_lines[i] = 'ENV GOWORK=off'
        if line.startswith('WORKDIR /app/services/'):
            new_lines[i] = '' # we handle it above
        if line.startswith(f'COPY services/{service_name} services/{service_name}'):
            new_lines[i] = '' # we handle it above
            
    final_lines = new_lines[:insert_idx] + deps + new_lines[insert_idx:]
    # Remove empty lines that we just nulled out
    final_lines = [l for l in final_lines if l is not None]
    
    with open(path, 'w') as f:
        f.write('\n'.join(final_lines))

for f in glob.glob('services/*/Dockerfile'):
    fix_dockerfile(f)
