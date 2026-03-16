# Self-Hosting Guide

Operator OS can be deployed as a single binary, via Docker Compose, or on Kubernetes with Helm.

---

## Option 1: Single Binary (Bare Metal)

The simplest deployment — a single static binary with no dependencies.

### Prerequisites

- Go 1.25+ (to build from source) or download a precompiled binary
- SQLite (embedded, no separate install needed)

### Install

```bash
# From source
git clone https://github.com/operatoronline/Operator-OS.git
cd Operator-OS
make deps && make build
make install    # Installs to ~/.local/bin/operator

# Or download a precompiled binary from GitHub Releases
# https://github.com/operatoronline/Operator-OS/releases
```

### Configure

```bash
# Create the config directory
mkdir -p ~/.operator

# Copy the example config
cp config/config.example.json ~/.operator/config.json

# Edit with your API keys and channel settings
$EDITOR ~/.operator/config.json
```

See [Configuration Reference](configuration.md) for all available options.

### Run

```bash
# Start the gateway (API server + channel handlers)
operator gateway

# Or run a one-off agent command
operator agent -m "Hello, what can you do?"
```

### Run as a systemd Service

Create `/etc/systemd/system/operator.service`:

```ini
[Unit]
Description=Operator OS Gateway
After=network.target

[Service]
Type=simple
User=operator
ExecStart=/home/operator/.local/bin/operator gateway
Restart=on-failure
RestartSec=5
Environment=TZ=UTC

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now operator
sudo systemctl status operator
```

---

## Option 2: Docker Compose

### Prerequisites

- Docker Engine 24+
- Docker Compose v2

### Minimal Deployment (Alpine-based)

Smallest image size, suitable for edge devices.

```bash
git clone https://github.com/operatoronline/Operator-OS.git
cd Operator-OS

# First run — generates default config
docker compose -f docker/docker-compose.yml --profile gateway up

# Edit the generated config
vim docker/data/config.json

# Start in background
docker compose -f docker/docker-compose.yml --profile gateway up -d
```

### Full Deployment (with MCP support)

Includes Node.js for MCP servers and tool extensions.

```bash
docker compose -f docker/docker-compose.full.yml --profile gateway up -d
```

### Development Stack (Web UI + Gateway)

For local development with the React frontend and Go backend:

```bash
# Start backend gateway + optional PostgreSQL/Redis
docker compose up -d

# Start the web UI dev server separately
cd web && npm install && npm run dev
```

### Full Production Stack

Includes the agent, managed services, and web UI:

```bash
docker compose -f docker-compose.managed.yml up -d
```

### Environment Variables

Create a `.env` file alongside the compose file:

```bash
cp .env.example .env
# Edit .env with your API keys
```

### Docker Build Targets

```bash
make docker-build          # Minimal Alpine image
make docker-build-full     # Full image with Node.js + MCP
make docker-run            # Run minimal gateway
make docker-run-full       # Run full gateway
make docker-clean          # Remove images and volumes
```

---

## Option 3: Kubernetes with Helm

For production deployments with scaling, monitoring, and high availability.

### Prerequisites

- Kubernetes 1.28+
- Helm 3.12+
- kubectl configured for your cluster

### Install the Chart

```bash
cd deploy/helm

# Install with default values
helm install operator-os ./operator-os

# Or with a custom values file
helm install operator-os ./operator-os -f values-prod.yaml
```

### Key Helm Values

| Value | Default | Description |
|-------|---------|-------------|
| `image.repository` | `docker.io/operatoronline/operator-os` | Container image |
| `image.tag` | `latest` | Image tag |
| `gateway.replicaCount` | `2` | Gateway replicas |
| `gateway.autoscaling.enabled` | `true` | Enable HPA for gateway |
| `worker.enabled` | `true` | Deploy worker pods |
| `worker.replicaCount` | `2` | Worker replicas |
| `ingress.enabled` | `false` | Enable Ingress |
| `postgresql.enabled` | `false` | Use PostgreSQL (vs SQLite) |
| `nats.enabled` | `false` | Use NATS JetStream |
| `redis.enabled` | `false` | Use Redis for caching |
| `metrics.enabled` | `true` | Enable Prometheus metrics |

### Production Configuration

Create `values-prod.yaml`:

```yaml
image:
  tag: "v1.0.0"

gateway:
  replicaCount: 3
  resources:
    requests:
      cpu: 200m
      memory: 256Mi
    limits:
      cpu: "2"
      memory: 1Gi

ingress:
  enabled: true
  className: nginx
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
  hosts:
    - host: operator.yourdomain.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - secretName: operator-tls
      hosts:
        - operator.yourdomain.com

postgresql:
  enabled: true

redis:
  enabled: true

secrets:
  encryptionKey: "<generate-a-random-32-byte-key>"
  jwtSecret: "<generate-a-random-secret>"
```

### Secrets Management

Store sensitive values in Kubernetes Secrets rather than `values.yaml`:

```bash
kubectl create secret generic operator-secrets \
  --from-literal=encryption-key="$(openssl rand -hex 32)" \
  --from-literal=jwt-secret="$(openssl rand -hex 32)" \
  --from-literal=openai-key="sk-..." \
  --from-literal=anthropic-key="sk-ant-..."
```

### Upgrade

```bash
helm upgrade operator-os ./operator-os -f values-prod.yaml
```

### Uninstall

```bash
helm uninstall operator-os
```

---

## Storage

### SQLite (Default)

SQLite is the default database. Data is stored in `~/.operator/data.db`. No external database required.

- Best for: single-user, edge devices, development
- Backup: copy the `.db` file
- No configuration needed

### PostgreSQL (Optional)

For multi-tenant or high-availability deployments:

```json
{
  "postgresql": {
    "dsn": "postgres://user:password@host:5432/operator?sslmode=require"
  }
}
```

Or set via the `pgDSN` Helm secret for Kubernetes deployments.

---

## Reverse Proxy

### nginx

```nginx
server {
    listen 443 ssl;
    server_name operator.yourdomain.com;

    ssl_certificate /etc/letsencrypt/live/operator.yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/operator.yourdomain.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:18790;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket support
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
```

### Caddy

```
operator.yourdomain.com {
    reverse_proxy 127.0.0.1:18790
}
```

---

## Hardware Requirements

| Deployment | CPU | RAM | Storage |
|-----------|-----|-----|---------|
| Edge (single agent) | 1 core, 0.6 GHz | 10 MB | 50 MB |
| Small (1–5 agents) | 1 core | 128 MB | 500 MB |
| Medium (5–50 agents) | 2 cores | 512 MB | 2 GB |
| Large (50+ agents, PostgreSQL) | 4+ cores | 2+ GB | 10+ GB |

Operator OS runs on x86_64, ARM64, ARMv7, RISC-V, and LoongArch64.
