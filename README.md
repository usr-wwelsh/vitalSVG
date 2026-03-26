# VitalSVG

Live SVG badges for your infrastructure. Like github-readme-stats, but for your actual servers.

```markdown
![plex status](https://your-host/badge/docker/plex/status.svg)
![plex cpu](https://your-host/badge/docker/plex/cpu.svg)
![plex ram](https://your-host/badge/docker/plex/ram.svg)
![plex uptime](https://your-host/badge/docker/plex/uptime.svg)
![plex cpu trend](https://your-host/badge/docker/plex/sparkline.svg?metric=cpu)
```

Polls Docker and Proxmox APIs, stores 24h of metrics in SQLite, renders cache-friendly SVG badges you can embed anywhere.

- **Health-aware colors** — green/yellow/red based on status and resource usage
- **Sparkline charts** — tiny inline trend graphs for CPU and RAM
- **Zero-config embeds** — just an `<img>` tag, no JS or auth needed
- **Web UI** — auto-discovers containers at `http://localhost:8080/` with copy-paste badge URLs
- **Single binary** — no runtime dependencies, SVG templates embedded

## Quick Start

```bash
docker compose up -d
```

Or run directly:

```bash
go build .
./vitalsvg
```

Open `http://localhost:8080` for the dashboard.

## Configuration

Copy `config.yaml.example` to `config.yaml`, or use environment variables:

| Variable | Default | Description |
|---|---|---|
| `VITALSVG_PORT` | `8080` | Server port |
| `VITALSVG_POLL_INTERVAL` | `30s` | How often to poll sources |
| `VITALSVG_DOCKER_SOCKET` | `/var/run/docker.sock` | Docker socket path |
| `VITALSVG_PROXMOX_HOST` | — | Proxmox host:port (enables Proxmox) |
| `VITALSVG_PROXMOX_TOKEN_ID` | — | API token ID (`user@realm!name`) |
| `VITALSVG_PROXMOX_TOKEN_SECRET` | — | API token secret |
| `VITALSVG_PROXMOX_SKIP_TLS` | `false` | Skip TLS verification |

## Badge Endpoints

```
GET /badge/{source}/{name}/status.svg
GET /badge/{source}/{name}/cpu.svg
GET /badge/{source}/{name}/ram.svg
GET /badge/{source}/{name}/uptime.svg
GET /badge/{source}/{name}/sparkline.svg?metric=cpu
GET /badge/{source}/{name}/sparkline.svg?metric=ram
```

`source` is `docker` or `proxmox`. `name` is the container/VM name.
