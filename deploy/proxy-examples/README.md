# Reverse proxy configuration examples

Crucible IAP ships with a bundled Caddy container for zero-config TLS. If you
already run a reverse proxy (nginx, Traefik, HAProxy, your own Caddy instance,
etc.) you can bypass the bundled one.

## Quick start

```bash
# Use your own reverse proxy
docker compose --profile external-proxy up -d

# Use your own reverse proxy + bundled Authentik IdP
docker compose --profile external-proxy --profile authentik up -d
```

The API will be available at `127.0.0.1:8080` and the UI at `127.0.0.1:3000`
on the Docker host. Configure your proxy to forward to those addresses.

Set `CRUCIBLE_API_EXPOSE=0.0.0.0` in `.env` only if your proxy runs on a
different host and needs to reach the container over the network.

## Examples in this directory

| File | Proxy |
|------|-------|
| `nginx.conf` | nginx (place in `/etc/nginx/sites-available/`) |
| `traefik.yml` | Traefik v3 (file provider or Docker labels) |
| `caddy-standalone.Caddyfile` | Caddy running outside Docker Compose |

## Important: Grafana iframe embedding

The `/monitoring` page embeds Grafana panels as iframes loaded from `/grafana/*`.
If your proxy sets `X-Frame-Options: DENY` or `Content-Security-Policy: frame-ancestors 'none'` globally, the browser will refuse to render the panels.

Apply those headers only to non-Grafana routes. In Caddy:

```caddy
@notgrafana not path /grafana/*
header @notgrafana X-Frame-Options "DENY"
```

In nginx, scope the header to the non-Grafana location blocks rather than the server block.

## Important: SSE log streaming

The live run log endpoint (`GET /api/v1/runs/:id/logs`) uses Server-Sent Events.
Your proxy **must** disable response buffering for this path, otherwise logs
will only appear after the run completes.

- **nginx:** `proxy_buffering off;` on the `/api` location
- **Traefik:** buffering is disabled by default for SSE
- **Caddy:** use `flush_interval -1` on the reverse_proxy directive
- **HAProxy:** `option http-server-close` + `timeout tunnel 3600s`
