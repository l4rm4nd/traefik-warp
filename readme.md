# Traefik Warp – Real Client IP behind Cloudflare / CloudFront

When Traefik runs **behind Cloudflare or AWS CloudFront (proxy/tunnel)**, the socket IP belongs to the edge, not the visitor. **Traefik Warp** securely resolves the **visitor’s real IP** and forwards it to your apps.

## What it does

- **Accepts provider headers (only when the edge socket IP is trusted):**
  - **Cloudflare:** `CF-Connecting-IP`, `CF-Visitor` (for `http|https`)
  - **CloudFront:** `Cloudfront-Viewer-Address` (`IP:port` or `[IPv6]:port`)
- **Emits standard proxy headers for your apps:**
  - Appends the visitor IP to **`X-Forwarded-For`** (keeps the chain)
  - Sets **`X-Real-IP`** to the visitor IP
  - Sets **`X-Forwarded-Proto`** from CF-Visitor or from TLS as a fallback
  - Adds **`X-Is-Trusted`** = `yes|no` for Cloudflare requests
- **Hardens security:**
  - Only trusts headers if the **remote socket IP** is in known Cloudflare/CloudFront CIDRs
  - **Strips spoofable** inbound forwarding headers before setting new ones

---

## Configuration

### Options

| Setting    | Type   | Required | Allowed values                      | Description                                                                 |
|-----------:|--------|----------|-------------------------------------|-----------------------------------------------------------------------------|
| `provider` | string | **yes**  | `auto`, `cloudfront`, `cloudflare`  | Selects which edge network to trust. `auto` = decide by the **socket IP**. |
| `trustIp`  | map    | no       | per-provider CIDR list              | **Extends** the built-in allowlists. Keys: `cloudflare`, `cloudfront`.     |

> **Note:** `trustIp` **extends** (does not replace) the official ranges. Avoid `0.0.0.0/0` or `::/0`.

---

### Enable the plugin (Plugin Catalog)

```yaml
experimental:
  plugins:
    traefikwarp:
      moduleName: github.com/l4rm4nd/traefik-warp
      version: v1.0.2
````

### Use the middleware (Dynamic config)

````
http:
  middlewares:
    warp-auto:
      plugin:
        traefikwarp:
          provider: auto
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
