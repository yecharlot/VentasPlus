/**
 * VentasPlus WA Bridge — Baileys (ligero)
 * Compatible con el cliente OpenWA reducido de VentasPlus API:
 *  POST /api/sessions
 *  POST /api/sessions/:id/start
 *  POST /api/sessions/:id/pairing-code  { phoneNumber }
 *  GET  /api/sessions/:id
 *  GET  /api/sessions/:id/qr
 *  POST /api/sessions/:id/messages/send-text
 *  POST /api/sessions/:id/messages/send-image
 * Auth: X-API-Key == process.env.API_MASTER_KEY (si está definida)
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

function publicSession(s) {
  if (!s) return null;
  return {
    id: s.id,
    name: s.name,
    status: s.status,
    phoneNumber: s.phoneNumber || null,
    pairingCode: s.pairingCode || null,
    hasQr: !!s.lastQr,
  };
}

async function ensureSocket(id, { phoneNumber } = {}) {
  let s = sessions.get(id);
  if (s?.sock && (s.status === 'ready' || s.status === 'qr_ready' || s.status === 'initializing')) {
    return s;
  }

  const dir = path.join(DATA, id);
  fs.mkdirSync(dir, { recursive: true });
  const { state, saveCreds } = await useMultiFileAuthState(dir);
  const { version } = await fetchLatestBaileysVersion();

  s = s || {
    id,
    name: id,
    status: 'initializing',
    phoneNumber: phoneNumber || null,
    pairingCode: null,
    lastQr: null,
    sock: null,
  };
  s.status = 'initializing';
  sessions.set(id, s);

  const sock = makeWASocket({
    version,
    auth: state,
    printQRInTerminal: false,
    logger: pino({ level: 'silent' }),
    browser: ['VentasPlus', 'Chrome', '120.0.0'],
  });
  s.sock = sock;

  sock.ev.on('creds.update', saveCreds);

  sock.ev.on('connection.update', async (update) => {
    const { connection, lastDisconnect, qr } = update;
    if (qr) {
      s.lastQr = qr;
      s.status = 'qr_ready';
      // Si hay número, pedir pairing code (sin QR)
      if (s.phoneNumber) {
        try {
          const code = await sock.requestPairingCode(s.phoneNumber);
          s.pairingCode = code;
          s.status = 'qr_ready';
          log.info({ id, code }, 'pairing code');
        } catch (e) {
          log.warn({ err: String(e) }, 'pairing code failed');
        }
      }
    }
    if (connection === 'open') {
      s.status = 'ready';
      s.pairingCode = null;
      s.lastQr = null;
      log.info({ id }, 'whatsapp ready');
    }
    if (connection === 'close') {
      const code = new Boom(lastDisconnect?.error)?.output?.statusCode;
      s.status = 'disconnected';
      s.sock = null;
      const restart = code !== DisconnectReason.loggedOut;
      log.warn({ id, code, restart }, 'connection closed');
      if (restart) {
        setTimeout(() => ensureSocket(id, { phoneNumber: s.phoneNumber }).catch(() => {}), 2000);
      } else {
        s.status = 'failed';
      }
    }
  });

  sessions.set(id, s);
  return s;
}

const app = express();
app.use(express.json({ limit: '15mb' }));
app.get('/health', (_, res) => res.json({ ok: true, service: 'ventasplus-wa-bridge' }));
app.get('/', (_, res) => res.json({ ok: true, service: 'ventasplus-wa-bridge', auth: !!API_KEY }));

app.use('/api', auth);

app.post('/api/sessions', async (req, res) => {
  const name = (req.body?.name || 'ventasplus').replace(/[^a-zA-Z0-9_-]/g, '') || 'ventasplus';
  const id = name;
  if (!sessions.has(id)) {
    sessions.set(id, { id, name, status: 'created', phoneNumber: null, pairingCode: null, lastQr: null, sock: null });
  }
  res.status(201).json({ id, name, status: sessions.get(id).status });
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

app.post('/api/sessions/:id/pairing-code', async (req, res) => {
  try {
    let phone = String(req.body?.phoneNumber || '').replace(/\D/g, '');
    if (phone.length < 8) return res.status(400).json({ error: 'phoneNumber required' });
    let s = sessions.get(req.params.id);
    if (!s) {
      sessions.set(req.params.id, { id: req.params.id, name: req.params.id, status: 'created', phoneNumber: phone, pairingCode: null, lastQr: null, sock: null });
    } else {
      s.phoneNumber = phone;
    }
    s = await ensureSocket(req.params.id, { phoneNumber: phone });
    // esperar código un poco
    for (let i = 0; i < 15; i++) {
      if (s.pairingCode) break;
      await new Promise((r) => setTimeout(r, 500));
      s = sessions.get(req.params.id);
    }
    if (!s.pairingCode && s.sock && s.status === 'qr_ready') {
      try {
        s.pairingCode = await s.sock.requestPairingCode(phone);
      } catch (e) {
        return res.status(409).json({ error: String(e), status: s.status });
      }
    }
    if (!s.pairingCode) {
      return res.status(409).json({
        error: 'pairing code not ready yet; retry',
        status: s.status,
      });
    }
    res.status(201).json({ pairingCode: s.pairingCode, status: s.status });
  } catch (e) {
    res.status(500).json({ error: String(e) });
  }
});

app.get('/api/sessions/:id/qr', async (req, res) => {
  const s = sessions.get(req.params.id);
  if (!s) return res.status(404).json({ error: 'not found' });
  if (!s.lastQr) return res.status(404).json({ error: 'no qr', status: s.status });
  const dataUrl = await qrcode.toDataURL(s.lastQr);
  res.json({ qr: dataUrl, status: s.status });
});

app.post('/api/sessions/:id/messages/send-text', async (req, res) => {
  const s = sessions.get(req.params.id);
  if (!s?.sock || s.status !== 'ready') return res.status(409).json({ error: 'session not ready', status: s?.status });
  let jid = String(req.body?.chatId || '');
  if (!jid.includes('@')) jid = jid.replace(/\D/g, '') + '@s.whatsapp.net';
  jid = jid.replace('@c.us', '@s.whatsapp.net');
  const text = String(req.body?.text || '');
  try {
    const r = await s.sock.sendMessage(jid, { text });
    res.status(201).json({ ok: true, messageId: r?.key?.id });
  } catch (e) {
    res.status(500).json({ ok: false, error: String(e) });
  }
});

app.post('/api/sessions/:id/messages/send-image', async (req, res) => {
  const s = sessions.get(req.params.id);
  if (!s?.sock || s.status !== 'ready') return res.status(409).json({ error: 'session not ready', status: s?.status });
  let jid = String(req.body?.chatId || '');
  if (!jid.includes('@')) jid = jid.replace(/\D/g, '') + '@s.whatsapp.net';
  jid = jid.replace('@c.us', '@s.whatsapp.net');
  const caption = String(req.body?.caption || '');
  const image = req.body?.image || {};
  try {
    let payload;
    if (image.url) {
      payload = { image: { url: image.url }, caption };
    } else if (image.base64) {
      payload = { image: Buffer.from(image.base64, 'base64'), caption };
    } else {
      return res.status(400).json({ error: 'image.url or image.base64 required' });
    }
    const r = await s.sock.sendMessage(jid, payload);
    res.status(201).json({ ok: true, messageId: r?.key?.id });
  } catch (e) {
    res.status(500).json({ ok: false, error: String(e) });
  }
});

app.get('/api/sessions/:id/groups', async (req, res) => {
  const s = sessions.get(req.params.id);
  if (!s?.sock || s.status !== 'ready') return res.status(409).json({ error: 'session not ready' });
  try {
    const groups = await s.sock.groupFetchAllParticipating();
    const list = Object.values(groups || {}).map((g) => ({ id: g.id, subject: g.subject }));
    res.json({ ok: true, groups: list });
  } catch (e) {
    res.status(500).json({ error: String(e) });
  }
});

app.listen(PORT, () => log.info({ PORT }, 'wa-bridge listening'));
