---
title: Kubernetes
description: Deploy dnsweaver in Kubernetes with RBAC, Helm, or Kustomize
icon: material/kubernetes
---

# Kubernetes Deployment

dnsweaver runs as a single-replica Deployment in Kubernetes, watching Ingress, IngressRoute, HTTPRoute, and Service resources for hostname extraction. It uses in-cluster authentication and requires read-only RBAC permissions.

## Prerequisites

- Kubernetes 1.26+
- `kubectl` access to the cluster
- DNS provider credentials (API tokens)

## Quick Start (Kustomize)

```bash
# Clone the repository
git clone https://github.com/maxfield-allison/dnsweaver.git
cd dnsweaver

# Edit the ConfigMap with your providers
$EDITOR deploy/kustomize/base/configmap.yaml

# Create the provider credentials Secret
kubectl create namespace dnsweaver
kubectl -n dnsweaver create secret generic dnsweaver-provider-credentials \
  --from-literal=DNSWEAVER_INTERNAL_TOKEN=your-token-here

# Deploy
kubectl apply -k deploy/kustomize/base/
```

## Helm

```bash
helm install dnsweaver deploy/helm/dnsweaver/ \
  --namespace dnsweaver --create-namespace \
  --set config.providers[0].name=internal \
  --set config.providers[0].type=technitium \
  --set config.providers[0].domains[0]="*.internal.example.com" \
  --set config.providers[0].config.url=http://dns:5380 \
  --set config.providers[0].config.zone=internal.example.com
```

Or use a values file:

```bash
helm install dnsweaver deploy/helm/dnsweaver/ \
  --namespace dnsweaver --create-namespace \
  -f my-values.yaml
```

See [`deploy/helm/dnsweaver/values.yaml`](https://github.com/maxfield-allison/dnsweaver/blob/main/deploy/helm/dnsweaver/values.yaml) for all options.

## RBAC

dnsweaver needs **read-only** cluster-wide access to networking resources. The minimum ClusterRole:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: dnsweaver
rules:
  - apiGroups: ["networking.k8s.io"]
    resources: ["ingresses"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["traefik.io"]
    resources: ["ingressroutes"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["gateway.networking.k8s.io"]
    resources: ["httproutes"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["services"]
    verbs: ["get", "list", "watch"]
```

!!! tip "Minimal RBAC"
    If you only use Traefik IngressRoutes, you can disable the other resource types and remove their RBAC rules. Set `rbac.watchIngress=false`, `rbac.watchHTTPRoute=false`, and `rbac.watchServices=false` in the Helm values.

## Configuration

### Platform Mode

Set `platform: kubernetes` in your config or use the `DNSWEAVER_PLATFORM` environment variable:

| Value | Behavior |
| :---- | :------- |
| `docker` | Watch Docker events only (default) |
| `kubernetes` | Watch Kubernetes resources only |
| `both` | Watch both Docker and Kubernetes simultaneously |

### Kubernetes Settings

All settings can be configured via YAML config file or environment variables:

| YAML Key | Environment Variable | Default | Description |
| :------- | :------------------- | :------ | :---------- |
| `kubernetes.kubeconfig` | `DNSWEAVER_K8S_KUBECONFIG` | _(empty)_ | Path to kubeconfig; empty = in-cluster |
| `kubernetes.namespaces` | `DNSWEAVER_K8S_NAMESPACES` | _(empty)_ | Comma-separated namespace filter; empty = all |
| `kubernetes.watch_ingress` | `DNSWEAVER_K8S_WATCH_INGRESS` | `true` | Watch `networking.k8s.io/v1` Ingress |
| `kubernetes.watch_ingressroute` | `DNSWEAVER_K8S_WATCH_INGRESSROUTE` | `true` | Watch `traefik.io/v1alpha1` IngressRoute |
| `kubernetes.watch_httproute` | `DNSWEAVER_K8S_WATCH_HTTPROUTE` | `true` | Watch `gateway.networking.k8s.io/v1` HTTPRoute |
| `kubernetes.watch_services` | `DNSWEAVER_K8S_WATCH_SERVICES` | `false` | Watch `v1` Service (opt-in) |
| `kubernetes.label_selector` | `DNSWEAVER_K8S_LABEL_SELECTOR` | _(empty)_ | Label selector filter |
| `kubernetes.annotation_filter` | `DNSWEAVER_K8S_ANNOTATION_FILTER` | _(empty)_ | Annotation key=value filter |

### Resource Types

| Resource | API Group | Hostname Source |
| :------- | :-------- | :-------------- |
| Ingress | `networking.k8s.io/v1` | `.spec.rules[].host` |
| IngressRoute | `traefik.io/v1alpha1` | Parsed from `Host()` matcher in `.spec.routes[].match` |
| HTTPRoute | `gateway.networking.k8s.io/v1` | `.spec.hostnames[]` |
| Service | `v1` | `LoadBalancer` ingress hostnames, `ExternalName` values |

## Annotations

dnsweaver supports per-resource annotation overrides using the `dnsweaver.dev/` prefix:

| Annotation | Values | Description |
| :--------- | :----- | :---------- |
| `dnsweaver.dev/enabled` | `true` / `false` | Enable/disable DNS management for this resource |
| `dnsweaver.dev/record-type` | `A`, `AAAA`, `CNAME`, `SRV`, `TXT` | Override the record type |
| `dnsweaver.dev/target` | IP or hostname | Override the DNS target |
| `dnsweaver.dev/ttl` | seconds (e.g., `300`) | Override the TTL |
| `dnsweaver.dev/provider` | provider name | Route to a specific provider |
| `dnsweaver.dev/proxied` | `true` / `false` | Enable Cloudflare proxy |
| `dnsweaver.dev/adopt` | `true` / `false` | Enable or disable existing-record adoption. Enabling requires the matching provider's `ADOPT_EXISTING_ALLOW_OVERRIDES` gate. Disabling is always honored. |

### Example: Ingress with Annotations

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: my-app
  annotations:
    dnsweaver.dev/enabled: "true"
    dnsweaver.dev/record-type: "A"
    dnsweaver.dev/target: "192.0.2.100"
    dnsweaver.dev/ttl: "300"
spec:
  rules:
    - host: app.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: my-app
                port:
                  number: 80
```

### Example: Traefik IngressRoute

```yaml
apiVersion: traefik.io/v1alpha1
kind: IngressRoute
metadata:
  name: my-app
  annotations:
    dnsweaver.dev/provider: "internal"
    dnsweaver.dev/target: "192.0.2.100"
spec:
  entryPoints:
    - websecure
  routes:
    - match: Host(`app.internal.example.com`)
      kind: Rule
      services:
        - name: my-app
          port: 80
```

## Health Endpoints

| Path | Description |
| :--- | :---------- |
| `/health` | Liveness probe — returns 200 when the process is running |
| `/ready` | Readiness probe — returns 200 when watchers are synced |
| `/metrics` | Prometheus metrics (when enabled) |

## Monitoring

Enable the ServiceMonitor for Prometheus scraping:

```yaml
# Helm values
serviceMonitor:
  enabled: true
  interval: 60s
```

## Troubleshooting

### Pod not starting

```bash
kubectl -n dnsweaver logs deploy/dnsweaver
kubectl -n dnsweaver describe pod -l app.kubernetes.io/name=dnsweaver
```

### RBAC errors

Look for `403 Forbidden` in logs. Verify the ClusterRoleBinding references the correct namespace:

```bash
kubectl get clusterrolebinding dnsweaver -o yaml
```

### No hostnames detected

1. Verify the resource type is being watched (`watch_ingress`, `watch_ingressroute`, etc.)
2. Check namespace filters — empty means all namespaces
3. Verify annotations: `dnsweaver.dev/enabled` defaults to `true` if absent
4. Check label/annotation selectors if configured
5. Run with `logging.level: debug` for detailed extraction logs
