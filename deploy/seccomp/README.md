# LoKey seccomp profiles (allowlist)

All profiles use `defaultAction: SCMP_ACT_ERRNO` (deny by default) and an explicit
allowlist.

| File | Service | Notes |
|------|---------|--------|
| `api.json` | API | Direct `/usr/local/bin/api` + wget healthcheck |
| `fortuna.json` | Fortuna | Same pattern |
| `virtio.json` | VirtIO | Includes `mknodat`/`unlinkat` |
| `controller.json` | Controllers | Includes `ioctl` for I2C |
| `observability.json` | Traefik, Prometheus, Grafana, Node Exporter | Broader Moby-derived |
| `cadvisor.json` | cAdvisor | Allows mount/setns; denies kexec/modules/ptrace |

LoKey profiles are sized for **Alpine PID1 binary** (no `/app-bin` copy entrypoint)
plus busybox healthchecks in **exec form** (`CMD ["wget", ...]`, not shell form).
`clone` uses Moby-style masked rules. `socket` denies `AF_VSOCK`.

## Regenerating LoKey traces

```bash
mkdir -p .seccomp-trace
docker compose -f docker-compose.yaml -f docker-compose.seccomp-trace.yaml \
  up -d --force-recreate api fortuna virtio controller-a controller-b
# exercise health/status/metrics, then:
docker compose -f docker-compose.yaml -f docker-compose.seccomp-trace.yaml stop \
  api fortuna virtio controller-a controller-b
docker compose up -d --force-recreate api fortuna virtio controller-a controller-b
```
