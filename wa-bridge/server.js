/**
 * VentasPlus WA Bridge — Baileys
 * Pairing code (sin QR) + QR de respaldo.
 */
import express from 'express';
import pino from 'pino';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';
import makeWASocket, {
  useMultiFileAuthState,
  DisconnectReason,
  fetchLatestBaileysVersion,
  Browsers,
} from '@whiskeysockets/baileys';
import { Boom } from '@hapi/boom';
import qrcode from 'qrcode';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const log = pino({ level: process.env.LOG_LEVEL || 'info' });
const PORT = Number(process.env.PORT || 2785);
const API_KEY = process.env.API_MASTER_KEY || process.env.OPENWA_API_KEY || '';
const DATA = process.env.SESSION_DATA_PATH || path.join(__dirname, 'data', 'sessions');
fs.mkdirSync(DATA, { recursive: true });

/** @type {Map<string, any>} */
const sessions = new Map();

function auth(req, res, next) {
  if (!API_KEY) return next();
  const k = req.header('X-API-Key') || '';
  if (k !== API_KEY) return res.status(401).json({ ok: false, error: 'unauthorized' });
  next();
}

function digitsPhone(s) {
  return String(s || '').replace(/\D/g, '');
}

function publicSession(s) {
  if (!s) return null;
  return {
    id: s.id,
    name: s.name,
    status: s.status,
    phoneNumber: s.phoneNumber || null,
    pairingCode: s.pairingCode || null,
    hasQr: !!s.lastQr,
    registered: !!s.registered,
  };
}

function wipeSessionDir(id) {
  const dir = path.join(DATA, id);
  try {
    fs.rmSync(dir, { recursive: true, force: true });
  } catch (_) {}
  fs.mkdirSync(dir, { recursive: true });
}

async function closeSocket(id) {
  const s = sessions.get(id);
  if (s?.sock) {
    try {
      s.sock.end(undefined);
    } catch (_) {}
    try {
      s.sock.ev.removeAllListeners();
    } catch (_) {}
  }
  if (s) {
    s.sock = null;
    s.status = 'created';
    s.pairingCode = null;
    s.lastQr = null;
  }
}

/**
 * Crea socket Baileys.
 * Para pairing code: printQRInTerminal false y requestPairingCode si !registered.
 */
async function ensureSocket(id, { phoneNumber, forceNew } = {}) {
  let s = sessions.get(id);
  if (forceNew) {
    await closeSocket(id);
    wipeSessionDir(id);
    sessions.delete(id);
    s = null;
  }

  if (s?.sock && s.status === 'ready') return s;
  if (s?.sock && !forceNew && (s.status === 'qr_ready' || s.status === 'initializing')) {
    if (phoneNumber) s.phoneNumber = digitsPhone(phoneNumber);
    return s;
  }

  const dir = path.join(DATA, id);
  fs.mkdirSync(dir, { recursive: true });
  const { state, saveCreds } = await useMultiFileAuthState(dir);
  const { version } = await fetchLatestBaileysVersion();

  s = {
    id,
    name: id,
    status: 'initializing',
    phoneNumber: digitsPhone(phoneNumber) || null,
    pairingCode: null,
    lastQr: null,
    registered: !!state.creds?.registered,
    sock: null,
  };
  sessions.set(id, s);

  const sock = makeWASocket({
    version,
    auth: state,
    printQRInTerminal: false,
    logger: pino({ level: 'silent' }),
    browser: Browsers.ubuntu('Chrome'),
    markOnlineOnConnect: false,
  });
  s.sock = sock;

  sock.ev.on('creds.update', saveCreds);

  s.contacts = s.contacts || {};
  s.chats = s.chats || {};
  s.groupsCache = s.groupsCache || [];

  sock.ev.on('contacts.upsert', (list) => {
    for (const c of list || []) {
      if (c?.id) s.contacts[c.id] = { id: c.id, name: c.notify || c.name || c.verifiedName || c.id };
    }
  });
  sock.ev.on('contacts.update', (list) => {
    for (const c of list || []) {
      if (c?.id) {
        const prev = s.contacts[c.id] || { id: c.id };
        s.contacts[c.id] = {
          id: c.id,
          name: c.notify || c.name || c.verifiedName || prev.name || c.id,
        };
      }
    }
  });
  sock.ev.on('chats.upsert', (list) => {
    for (const c of list || []) {
      if (c?.id) s.chats[c.id] = { id: c.id, name: c.name || c.id, unread: c.unreadCount || 0 };
    }
  });
  sock.ev.on('chats.update', (list) => {
    for (const c of list || []) {
      if (c?.id) {
        const prev = s.chats[c.id] || { id: c.id };
        s.chats[c.id] = { id: c.id, name: c.name || prev.name || c.id, unread: c.unreadCount ?? prev.unread };
      }
    }
  });

  sock.ev.on('connection.update', async (update) => {
    const { connection, lastDisconnect, qr } = update;
    if (qr) {
      s.lastQr = qr;
      if (s.status !== 'ready') s.status = 'qr_ready';
    }
    if (connection === 'open') {
      s.status = 'ready';
      s.registered = true;
      s.pairingCode = null;
      s.lastQr = null;
      log.info({ id }, 'whatsapp ready');
      try {
        const groups = await sock.groupFetchAllParticipating();
        s.groupsCache = Object.values(groups || {}).map((g) => ({
          id: g.id,
          name: g.subject || g.id,
          type: 'group',
        }));
        log.info({ id, n: s.groupsCache.length }, 'groups cached');
      } catch (e) {
        log.warn({ err: String(e) }, 'group fetch failed');
      }
    }
    if (connection === 'close') {
      const statusCode = new Boom(lastDisconnect?.error)?.output?.statusCode;
      s.status = 'disconnected';
      s.sock = null;
      const loggedOut = statusCode === DisconnectReason.loggedOut;
      log.warn({ id, statusCode, loggedOut }, 'connection closed');
      if (!loggedOut) {
        setTimeout(() => {
          ensureSocket(id, { phoneNumber: s.phoneNumber }).catch((e) =>
            log.warn({ err: String(e) }, 'reconnect failed')
          );
        }, 2500);
      } else {
        s.status = 'failed';
      }
    }
  });

  // Pairing code: solo si aún no está registrado (docs Baileys)
  if (!state.creds?.registered && s.phoneNumber) {
    try {
      // pequeño delay para que el socket inicie el handshake
      await new Promise((r) => setTimeout(r, 1500));
      const code = await sock.requestPairingCode(s.phoneNumber);
      s.pairingCode = String(code || '').toUpperCase();
      s.status = 'qr_ready';
      log.info({ id, code: s.pairingCode }, 'pairing code issued');
    } catch (e) {
      log.warn({ err: String(e) }, 'requestPairingCode failed');
      s.status = 'qr_ready'; // puede quedar QR como respaldo
    }
  } else if (state.creds?.registered) {
    s.status = 'initializing'; // esperando open
  }

  sessions.set(id, s);
  return s;
}

const app = express();
app.use(express.json({ limit: '15mb' }));

app.get('/health', (_, res) => res.json({ ok: true, service: 'ventasplus-wa-bridge' }));
app.get('/', (_, res) =>
  res.json({
    ok: true,
    service: 'ventasplus-wa-bridge',
    auth: !!API_KEY,
    tip: 'Usa pairing-code con número país+número sin +',
  })
);

app.use('/api', auth);

app.post('/api/sessions', async (req, res) => {
  const name = (req.body?.name || 'ventasplus').replace(/[^a-zA-Z0-9_-]/g, '') || 'ventasplus';
  if (!sessions.has(name)) {
    sessions.set(name, {
      id: name,
      name,
      status: 'created',
      phoneNumber: null,
      pairingCode: null,
      lastQr: null,
      sock: null,
    });
  }
  res.status(201).json({ id: name, name, status: sessions.get(name).status });
});

app.get('/api/sessions/:id', (req, res) => {
  const s = sessions.get(req.params.id);
  if (!s) return res.status(404).json({ error: 'not found' });
  res.json(publicSession(s));
});

app.post('/api/sessions/:id/start', async (req, res) => {
  try {
    const s = await ensureSocket(req.params.id);
    res.json(publicSession(s));
  } catch (e) {
    res.status(500).json({ error: String(e) });
  }
});

/** Reinicia sesión (borra credenciales) — útil si “no se puede vincular” */
app.post('/api/sessions/:id/reset', async (req, res) => {
  try {
    await closeSocket(req.params.id);
    wipeSessionDir(req.params.id);
    sessions.delete(req.params.id);
    res.json({ ok: true, status: 'reset' });
  } catch (e) {
    res.status(500).json({ error: String(e) });
  }
});

app.post('/api/sessions/:id/pairing-code', async (req, res) => {
  try {
    const phone = digitsPhone(req.body?.phoneNumber);
    if (phone.length < 10 || phone.length > 15) {
      return res.status(400).json({
        ok: false,
        error: 'Número inválido. Usa código de país + número, solo dígitos (ej. 56912345678). Sin 0 inicial del local.',
      });
    }

    // Siempre sesión limpia al pedir código nuevo
    await closeSocket(req.params.id);
    wipeSessionDir(req.params.id);
    sessions.delete(req.params.id);

    const s = await ensureSocket(req.params.id, { phoneNumber: phone, forceNew: true });

    // Esperar código hasta ~12s
    for (let i = 0; i < 24; i++) {
      if (s.pairingCode) break;
      await new Promise((r) => setTimeout(r, 500));
    }

    // Reintento explícito
    if (!s.pairingCode && s.sock && !s.registered) {
      try {
        await new Promise((r) => setTimeout(r, 800));
        s.pairingCode = String(await s.sock.requestPairingCode(phone)).toUpperCase();
      } catch (e) {
        log.warn({ err: String(e) }, 'retry pairing failed');
      }
    }

    if (!s.pairingCode) {
      return res.status(409).json({
        ok: false,
        error:
          'No se generó el código. Prueba de nuevo en 10s. En el teléfono: WhatsApp → Dispositivos vinculados → Vincular con el número de teléfono.',
        status: s.status,
        hasQr: !!s.lastQr,
        sessionId: s.id,
      });
    }

    res.status(201).json({
      ok: true,
      pairingCode: s.pairingCode,
      status: s.status,
      sessionId: s.id,
      instructions:
        'En el MISMO número: WhatsApp → Menú → Dispositivos vinculados → Vincular un dispositivo → Vincular con el número de teléfono → escribe el código (caduca en ~1 min).',
    });
  } catch (e) {
    res.status(500).json({ ok: false, error: String(e) });
  }
});

app.get('/api/sessions/:id/qr', async (req, res) => {
  const s = sessions.get(req.params.id);
  if (!s) return res.status(404).json({ error: 'not found' });
  if (!s.lastQr) {
    // intentar levantar socket para obtener QR
    try {
      await ensureSocket(req.params.id, { forceNew: !s.sock });
      for (let i = 0; i < 20 && !s.lastQr; i++) await new Promise((r) => setTimeout(r, 400));
    } catch (_) {}
  }
  if (!s.lastQr) return res.status(404).json({ error: 'no qr yet', status: s?.status });
  const dataUrl = await qrcode.toDataURL(s.lastQr);
  res.json({ qr: dataUrl, status: s.status });
});

app.post('/api/sessions/:id/messages/send-text', async (req, res) => {
  const s = sessions.get(req.params.id);
  if (!s?.sock || s.status !== 'ready') {
    return res.status(409).json({ error: 'session not ready', status: s?.status });
  }
  let jid = String(req.body?.chatId || '');
  if (!jid.includes('@')) jid = digitsPhone(jid) + '@s.whatsapp.net';
  jid = jid.replace('@c.us', '@s.whatsapp.net');
  try {
    const r = await s.sock.sendMessage(jid, { text: String(req.body?.text || '') });
    res.status(201).json({ ok: true, messageId: r?.key?.id });
  } catch (e) {
    res.status(500).json({ ok: false, error: String(e) });
  }
});

app.post('/api/sessions/:id/messages/send-image', async (req, res) => {
  const s = sessions.get(req.params.id);
  if (!s?.sock || s.status !== 'ready') {
    return res.status(409).json({ error: 'session not ready', status: s?.status });
  }
  let jid = String(req.body?.chatId || '');
  if (!jid.includes('@')) jid = digitsPhone(jid) + '@s.whatsapp.net';
  jid = jid.replace('@c.us', '@s.whatsapp.net');
  const caption = String(req.body?.caption || '');
  const image = req.body?.image || {};
  try {
    let payload;
    if (image.url) payload = { image: { url: image.url }, caption };
    else if (image.base64) payload = { image: Buffer.from(image.base64, 'base64'), caption };
    else return res.status(400).json({ error: 'image.url or image.base64 required' });
    const r = await s.sock.sendMessage(jid, payload);
    res.status(201).json({ ok: true, messageId: r?.key?.id });
  } catch (e) {
    res.status(500).json({ ok: false, error: String(e) });
  }
});

app.get('/api/sessions/:id/groups', async (req, res) => {
  const s = sessions.get(req.params.id);
  if (!s?.sock || s.status !== 'ready') return res.status(409).json({ error: 'session not ready', status: s?.status });
  try {
    const groups = await s.sock.groupFetchAllParticipating();
    const list = Object.values(groups || {}).map((g) => ({ id: g.id, subject: g.subject, name: g.subject }));
    s.groupsCache = list.map((g) => ({ id: g.id, name: g.subject || g.id, type: 'group' }));
    res.json({ ok: true, groups: list });
  } catch (e) {
    res.status(500).json({ error: String(e) });
  }
});

/** Grupos + contactos/chats para elegir destino sin escribir */
app.get('/api/sessions/:id/destinations', async (req, res) => {
  const s = sessions.get(req.params.id);
  if (!s?.sock || s.status !== 'ready') {
    return res.status(409).json({ ok: false, error: 'session not ready', status: s?.status });
  }
  try {
    // refrescar grupos
    try {
      const groups = await s.sock.groupFetchAllParticipating();
      s.groupsCache = Object.values(groups || {}).map((g) => ({
        id: g.id,
        name: g.subject || g.id,
        type: 'group',
      }));
    } catch (_) {}

    const groupList = (s.groupsCache || []).slice().sort((a, b) => (a.name || '').localeCompare(b.name || ''));

    // contactos 1:1 (no grupos)
    const contactList = Object.values(s.contacts || {})
      .filter((c) => c.id && !String(c.id).endsWith('@g.us') && !String(c.id).includes('status@'))
      .map((c) => ({
        id: String(c.id).replace('@s.whatsapp.net', '@c.us'),
        name: c.name || c.id,
        type: 'contact',
      }))
      .sort((a, b) => (a.name || '').localeCompare(b.name || ''));

    // chats recientes que no estén ya en contactos
    const seen = new Set(contactList.map((c) => c.id));
    for (const ch of Object.values(s.chats || {})) {
      if (!ch.id || String(ch.id).endsWith('@g.us')) continue;
      const id = String(ch.id).replace('@s.whatsapp.net', '@c.us');
      if (seen.has(id)) continue;
      contactList.push({ id, name: ch.name || id, type: 'chat' });
      seen.add(id);
    }
    contactList.sort((a, b) => (a.name || '').localeCompare(b.name || ''));

    res.json({
      ok: true,
      groups: groupList,
      contacts: contactList.slice(0, 300),
      total: groupList.length + Math.min(contactList.length, 300),
    });
  } catch (e) {
    res.status(500).json({ ok: false, error: String(e) });
  }
});

app.listen(PORT, () => log.info({ PORT }, 'wa-bridge listening'));
