# VentasPlus

Publicación cruzada para gestores de ventas: **captura de pantalla real** → editar → publicar a **Facebook** (Graph API) y **WhatsApp** (OpenWA), con persistencia en **Cloudflare Durable Objects**.

## Arquitectura

| Pieza | Tecnología | Rol |
|-------|------------|-----|
| PWA | `frontend/publisher-pwa/index.html` | Captura `getDisplayMedia`, recorte, texto, publicar |
| API | Go `backend/cmd/ventasplus` | `POST /api/publish` |
| WhatsApp | OpenWA (self-hosted) | Envío texto/imagen a chats/grupos |
| Facebook | Graph API v19 | Feed / fotos en páginas o grupos (según token) |
| Persistencia | Cloudflare Worker + DO | Publicaciones, agentes, destinos |

## Arranque local

```bash
# API
cd backend && go run ./cmd/ventasplus

# PWA (cualquier static server)
cd frontend/publisher-pwa && python3 -m http.server 5173

# OpenWA (opcional)
cd docker/openwa && docker compose up
```

Configura `.env` desde `.env.example`. En la PWA, ajusta `API_BASE` al backend.

## Cloudflare

```bash
cd cloudflare
npx wrangler deploy
```

## Flujo

1. Abrir PWA → botón **+**
2. Compartir pantalla / ventana (`getDisplayMedia`)
3. Recortar + texto
4. Nombre, precio, descripción
5. Destinos Facebook / WhatsApp (IDs)
6. Publicar → API → FB + WA + DO

## Tokens necesarios (producción)

- `FACEBOOK_ACCESS_TOKEN` — token de página con `pages_manage_posts` (grupos requieren permisos adicionales y a menudo app en revisión).
- `OPENWA_URL` + `OPENWA_API_KEY` + `OPENWA_SESSION_ID` — tras escanear QR en OpenWA.
- Worker URL en `CLOUDFLARE_WORKERS_URL`.

## Nota legal / ToS

WhatsApp no oficial (OpenWA) y publicación automatizada en Facebook están sujetos a términos de cada plataforma. Usa cuentas y permisos propios; no uses el sistema para spam.
