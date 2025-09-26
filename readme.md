# Real IP from Cloudflare/AWS Cloudfront Proxy/Tunnel

If Traefik is behind a Cloudflare/AWS Cloudfront Proxy/Tunnel, it won't be able to get the real IP from the external client as well as other information.

Processed Headers:
- Cloudflare: CF-Connecting-IP
- Cloudfront: Cloudfront-Viewer-Address

## Configuration

### Configuration documentation

Supported configurations per body

| Setting             | Allowed values | Required | Description                                         |
| :------------------ | :------------- | :------- | :-------------------------------------------------- |
| provider            | string         | yes      | auto, cloudfront, cloudflare                        |


### Enable the plugin

```yaml
experimental:
  plugins:
    traefikdisolver:
      modulename: github.com/kyaxcorp/traefikdisolver
      version: v1.0.9
```

### Plugin configuration

```yaml
http:
  middlewares:
    traefikdisolver-auto:
      plugin:
        traefikdisolver:
          provider: auto

    traefikdisolver-cloudfront:
      plugin:
        traefikdisolver:
          provider: cloudfront

    traefikdisolver-cloudflare:
      plugin:
        traefikdisolver:
          provider: cloudflare

  routers:
    my-router-auto:
      rule: PathPrefix(`/`) && (Host(`cloudfront.example.com`) || Host(`cloudflare.example.com`))
      service: service-whoami
      entryPoints:
        - http
      middlewares:
        - traefikdisolver-cloudfront
    my-router-cloudfront:
      rule: PathPrefix(`/`) && Host(`cloudfront.example.com`)
      service: service-whoami
      entryPoints:
        - http
      middlewares:
        - traefikdisolver-cloudfront
    my-router-cloudflare:
      rule: PathPrefix(`/`) && Host(`cloudflare.example.com`)
      service: service-whoami
      entryPoints:
        - http
      middlewares:
        - traefikdisolver-cloudflare

  services:
    service-whoami:
      loadBalancer:
        servers:
          - url: http://127.0.0.1:5000
```