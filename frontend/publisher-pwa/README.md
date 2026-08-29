
## WhatsApp: dos modos

### A) OpenWA — código de emparejamiento (sin QR)
1. Despliega OpenWA y configura `OPENWA_URL` + `OPENWA_API_KEY` en la API.
2. En la PWA: número con código de país → **Obtener código**.
3. En el teléfono: WhatsApp → Dispositivos vinculados → **Vincular con número** → escribe el código de 8 caracteres.

### B) Cloud API (Meta) — sin vincular WhatsApp personal
Configura en la API:
```
WHATSAPP_CLOUD_TOKEN=...
WHATSAPP_CLOUD_PHONE_NUMBER_ID=...
```
Los mensajes salen del **número de negocio**. Requiere cuenta Meta Business (una sola vez, del dueño del producto).

### C) Sin servidor WhatsApp
La PWA sigue pudiendo **compartir** la oferta desde el teléfono.
