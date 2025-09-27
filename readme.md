# Traefik Warp – Real Client IP behind Cloudflare / CloudFront

When Traefik runs **behind Cloudflare or AWS CloudFront (proxy/tunnel)**, the socket IP belongs to the edge, not the visitor. Due to this, your backend services and log files receive the incorrect edge IP and not the real visitor's IP address.

## What it does

- **Accepts provider headers (only when the edge socket IP is trusted):**
  - **Cloudflare:** `CF-Connecting-IP`, `CF-Visitor` (scheme)
  - **CloudFront:** `Cloudfront-Viewer-Address` (`IP:port` or `[IPv6]:port`)
- **Emits standard proxy headers for your backend apps:**
  - Appends the visitor IP to **`X-Forwarded-For`** (keeps the chain)
  - Sets **`X-Real-IP`** to the visitor IP
  - Sets **`X-Forwarded-Proto`** to **`http`** or **`https`** (from `CF-Visitor` when present, otherwise TLS fallback)
  - Adds neutral markers: **`X-Warp-Trusted`** = `yes|no`, **`X-Warp-Provider`** = `cloudflare|cloudfront|unknown`
- **Hardens security:**
  - Only trusts headers if the **remote socket IP** is in known Cloudflare/CloudFront CIDRs
  - **Strips spoofable** inbound forwarding headers before setting new ones

## How it works

Traefik Warp automatically retrieves the latest Cloudflare and AWS CloudFront IPv4/IPv6 CIDR ranges from their official endpoints and builds an in-memory allowlist. On every request, it validates the remote socket IP against this allowlist. Only when it matches, the middleware trusts provider headers to resolve the visitor’s real IP, normalize `X-Forwarded-Proto` to `http` or `https`, and set `X-Forwarded-For`, `X-Real-IP`, `X-Warp-Trusted`, and `X-Warp-Provider`. The resolved address is then propagated to backend services and recorded in access logs. If the ranges cannot be fetched, the middleware stays safe (no public ranges are trusted) and you can extend trust explicitly via `trustIp`.

The custom HTTP headers `X-Warp-Trusted` and `X-Warp-Provider` are forwarded to your backends to document Traefik Warp’s decision. `X-Warp-Trusted` is `yes` when the socket IP matched the allowlist (so provider headers were trusted) and `no` otherwise. `X-Warp-Provider` identifies which network the socket IP matched - e.g. `cloudflare`, `cloudfront` or `unknown`. These headers are informational for logging, metrics, and policy decisions; they don’t affect how Traefik Warp validates or rewrites request headers.

---

## Configuration

### Options

| Setting            | Type   | Required | Allowed values                      | Description                                                                                               |
|-------------------:|--------|----------|-------------------------------------|-----------------------------------------------------------------------------------------------------------|
| `provider`         | string | **yes**  | `auto`, `cloudfront`, `cloudflare`  | Selects which edge network to trust. `auto` = decide by the **socket IP**.                                |
| `trustip`          | map    | no       | per-provider CIDR list              | **Extends** the built-in allowlists. Keys: `cloudflare`, `cloudfront`.                                    |
| `autoRefresh`      | bool   | no       | `true` / `false`                    | Periodically refresh Cloudflare/CloudFront CIDR ranges. **Default:** `true`.                              |
| `refreshInterval`  | string | no       | Go duration (e.g. `5m`, `1h`, `12h`)| Interval for auto refresh, used only when `autoRefresh` is true. **Default:** `12h`.                      |
| `debug`            | bool   | no       | `true` / `false`                    | Emit Traefik-style logs from the plugin (e.g., CIDR loads/refresh). **Default:** `false`.                 |

> **Note:** `trustIp` **extends** (does not replace) the official ranges. Avoid `0.0.0.0/0` or `::/0`.

---

### Enable the plugin (Plugin Catalog)

```yaml
experimental:
  plugins:
    traefikwarp:
      moduleName: github.com/l4rm4nd/traefik-warp
      version: v1.1.0
````

### Use the middleware (Dynamic config)

````
http:
  middlewares:
    warp-auto:
      plugin:
        traefikwarp:
          provider: auto
          autoRefresh: true
          refreshInterval: 24h
          debug: false
          # trustIp:                # optional: extend allow-lists
          #   cloudflare:
          #     - "198.51.100.0/24"
          #   cloudfront:
          #     - "203.0.113.0/24"

    warp-cloudfront:
      plugin:
        traefikwarp:
          provider: cloudfront

    warp-cloudflare:
      plugin:
        traefikwarp:
          provider: cloudflare

  routers:
    my-router-auto:
      rule: PathPrefix(`/`) && (Host(`cloudfront.example.com`) || Host(`cloudflare.example.com`))
      entryPoints: [web]     # or your entrypoint name(s)
      service: svc-whoami
      middlewares:
        - warp-auto

    my-router-cloudfront:
      rule: PathPrefix(`/`) && Host(`cloudfront.example.com`)
      entryPoints: [web]
      service: svc-whoami
      middlewares:
        - warp-cloudfront

    my-router-cloudflare:
      rule: PathPrefix(`/`) && Host(`cloudflare.example.com`)
      entryPoints: [web]
      service: svc-whoami
      middlewares:
        - warp-cloudflare

  services:
    svc-whoami:
      loadBalancer:
        servers:
          - url:http://127.0.0.1:5000
````

### Credits

Original code and idea from https://github.com/kyaxcorp/traefikdisolver
