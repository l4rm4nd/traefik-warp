# TraefikWarp – Real Client IP

A Traefik middleware plugin to automatically obtain the real visitor's IP address if Traefik is run behind a Content Delivery Network (CDN) like Cloudflare or CloudFront. Fully automated by fetching and regularly updating the official CDN CIDR IP addresses from official HTTP endpoints.

> [!CAUTION]
> This plugin will not help logging the visitor's real IP address in Traefik's access log.
> 
> For such cases, use [ProxyProtocol](https://doc.traefik.io/traefik/reference/install-configuration/entrypoints/#proxyprotocol-and-load-balancers) and define `trustedIPs` at your Traefik entrypoints. Example [here](https://github.com/Haxxnet/Compose-Examples/blob/main/examples/traefik/traefik.yml#L61).

## Features

- 🔒 **Trust-gated by edge IP**
  - Only honors headers when the **socket IP** matches Cloudflare/CloudFront edge CIDR IPs.

- 📥 **Provider headers supported**
  - **Cloudflare:** `CF-Connecting-IP`, `CF-Visitor` (scheme)  
  - **CloudFront:** `Cloudfront-Viewer-Address` (`IP:port` / `[IPv6]:port`)

- 📤 **Standard proxy headers emitted**  
  - Sets **`X-Real-IP`** to the visitor IP  
  - Appends visitor IP to **`X-Forwarded-For`** (preserves chain, only when trusted)  
  - Normalizes **`X-Forwarded-Proto`** to `http`/`https`

- 🧹 **Header hygiene**  
  - Strips spoofable inbound headers (**`X-Forwarded-For`**, **`X-Real-IP`**, **`X-Forwarded-Proto`**, `Forwarded`) before setting trusted values.

- 🏷️ **Neutral telemetry**  
  - Adds **`X-Warp-Trusted`** = `yes|no` and **`X-Warp-Provider`** = `cloudflare|cloudfront|unknown` for downstream logging/metrics.

- 🔁 **Auto CIDR refresh (enabled per default)**  
  - Periodically refreshes Cloudflare/CloudFront CIDRs (default **12h**) with configurable interval and optional debug logs.
  - No need to manually restart Traefik or re-initiate the plugin

## How it works

TraefikWarp automatically fetches the latest Cloudflare and AWS CloudFront IPv4/IPv6 CIDR ranges from their official endpoints and builds an in-memory allowlist. On every middleware request, it validates the remote socket IP against this allowlist. Only when it matches, the middleware trusts the specific provider's headers to resolve the visitor’s real IP address. It then normalizes `X-Forwarded-Proto` to `http` or `https` and sets `X-Forwarded-For`, `X-Real-IP`, `X-Warp-Trusted`, and `X-Warp-Provider`. The resolved address is then propagated to backend services and recorded in the backend service's access logs. CDN CIDR IP addresses are regularly refreshed (default every 12h). If the ranges cannot be fetched, the middleware stays safe as no public ranges are trusted per default. You may extend the allowlist of trusted IPs by using `trustIp`.

The custom HTTP headers `X-Warp-Trusted` and `X-Warp-Provider` are forwarded to your backends to document TraefikWarp’s decision. `X-Warp-Trusted` is `yes` when the socket IP matched the allowlist (so provider headers were trusted) and `no` otherwise. `X-Warp-Provider` identifies, which provider's network the socket IP matched - e.g. `cloudflare`, `cloudfront` or `unknown`. These headers are informational for logging, metrics, and policy decisions. They don’t affect how TraefikWarp validates or rewrites request headers.
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
      version: v1.1.3
```

### Use the middleware (Dynamic config)

```yaml
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
```

### Local Development

You can also enable this plugin during development as follows:

<details>

Clone this repository:

```bash
cd /tmp
git clone https://github.com/l4rm4nd/traefik-warp
```

Then mount the plugin dir as docker bind mount volume in Traefik's compose:

```yaml
    volumes:
      - /tmp/traefik-warp:/plugins-local/src/github.com/l4rm4nd/traefik-warp:ro
```

Enable the local plugin in Traefik's static config:

```yaml
experimental:
  localPlugins:
    traefikwarp:
      moduleName: github.com/l4rm4nd/traefik-warp
```

Finally, define the middleware in Traefik's dynamic config:

```yaml
http:
  middlewares:
    warp-auto:
      plugin:
        traefikwarp:
          provider: auto
          autoRefresh: true
          refreshInterval: 1m
          debug: true
```

And test it using a whoami container:

```yaml
services:

  whoami:
    image: traefik/whoami
    container_name: whoami
    hostname: whoami
    restart: unless-stopped
    expose:
      - 80
    environment:
      - WHOAMI_NAME=whoami
      - WHOAMI_PORT_NUMBER=80
    networks:
      - proxy # change to your traefik network
    labels:
      - traefik.enable=true
      - traefik.docker.network=proxy # change to your traefik network
      - traefik.http.routers.whoami.rule=Host(`whoami.example.com`)
      - traefik.http.services.whoami.loadbalancer.server.port=80
      - traefik.http.routers.whoami.middlewares=warp-auto@file # change to correct middleware name
```

The plugin will emit debug messages if you have enabled `debug`:

```conf
2025-09-27T03:59:58+02:00 INF warp: CIDRs loaded cf=22 cfn=194 middleware=warp-auto@file module=github.com/l4rm4nd/traefik-warp plugin=plugin-traefikwarp
2025-09-27T04:01:04+02:00 INF warp: refreshed CIDRs cf=22 cfn=194 module=github.com/l4rm4nd/traefik-warp plugin=plugin-traefikwarp
```

</details>

### Credits

Original code and idea from https://github.com/kyaxcorp/traefikdisolver
