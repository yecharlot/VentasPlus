# VentasPlus

Publicación para gestores de ventas **sin App de Meta del dueño del producto**.

| Canal | Cómo |
|-------|------|
| **Facebook** | El usuario usa **Compartir** del teléfono (Web Share) o descarga imagen + texto |
| **WhatsApp** | Opcional: OpenWA + QR (como WhatsApp Web); si no, también Compartir |

## Local

```bash
cd backend && go run ./cmd/ventasplus
cd frontend/publisher-pwa && python3 -m http.server 5173
```

## Cloudflare (historial)

Worker: `https://ventasplus-workers.lhmolam-877.workers.dev`

```bash
export CLOUDFLARE_WORKERS_URL=https://ventasplus-workers.lhmolam-877.workers.dev
```

## OpenWA (opcional)

Sin OpenWA la app sigue útil (compartir nativo). Con OpenWA: define `OPENWA_URL`, `OPENWA_API_KEY`, `OPENWA_SESSION_ID`.
