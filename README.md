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

## Compartir desde WhatsApp → VentasPlus (Share Target)

1. Abre https://ventasplus.onrender.com en **Chrome Android**.
2. Menú ⋮ → **Instalar aplicación** / Añadir a inicio.
3. En WhatsApp: mantén una foto → **Compartir** → elige **VentasPlus**.
4. Completa precio/nombre → envía la card.

**Límites:** funciona bien en **Android + Chrome** con la PWA instalada. En **iPhone/Safari** Apple casi no permite Share Target para PWAs; haría falta app nativa.

## Funciones avanzadas (sesión WhatsApp vinculada)

- **Bandeja de fotos de grupos:** elige grupo → fotos recientes en caché → un toque → oferta.
- **Multi-destino:** varios grupos/contactos por publicación (tope configurable).
- **Plantillas** de texto (`/api/templates`).
- **Historial** de publicaciones (`/api/publications`).
- **Límites anti-bloqueo** (`/api/whatsapp/limits`):
  - `WA_MAX_DESTINATIONS` (default 5)
  - `WA_MIN_DELAY_MS` (default 3500)
  - `WA_MAX_SENDS_PER_HOUR` (default 25)
  - `WA_MAX_SENDS_PER_DAY` (default 80)

Las fotos de grupo se acumulan cuando el puente ve mensajes nuevos tras vincular (no es un historial completo de WhatsApp).

## Alset Mind (opcional)

El puente WhatsApp puede notificar a Alset Mind cada mensaje relevante:

```
ALSET_MIND_WEBHOOK=https://tu-nodo/api/mind/tick
```

Ideas de uso:
- Episodios de memoria CID desde chats de proveedores (precios, productos).
- Órgano curiosity cuando aparece un tema nuevo en un grupo.
- Sugerir texto de oferta con LISPAI a partir del historial del chat (sin LLM: plantillas + corpus).

## Limitaciones de WhatsApp (importante)

- WhatsApp **no autoriza** clientes tipo Baileys/OpenWA. Puede mostrar **aviso de estafa/seguridad** al vincular y **cerrar la sesión** en segundos o minutos.
- En **Render free** el proceso se duerme: la sesión se pierde y hay que volver a vincular.
- La única vía estable y oficial es **WhatsApp Cloud API** (número de negocio Meta).
- Contactos: se listan los que WhatsApp sincroniza al puente; la agenda completa no siempre llega.

## Modo seguro (por defecto)

Sin vincular WhatsApp. Flujo:

1. Crear oferta (foto + precio + detalle)
2. **Compartir** del sistema → el usuario elige WhatsApp

El puente OpenWA/Baileys queda experimental y **no se recomienda** con números personales.
