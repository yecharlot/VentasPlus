/**
 * VentasPlus Workers — persistencia en un Durable Object (free plan: sqlite classes).
 * Routes:
 *   POST /api/publications
 *   GET  /api/publications/agent/:agentId
 *   POST /api/agents
 *   GET  /api/agents/:id
 *   POST /api/destinations
 *   GET  /api/destinations/agent/:agentId
 *   GET  /health
 */

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    const path = url.pathname;
    if (request.method === "OPTIONS") {
      return new Response(null, {
        headers: {
          "Access-Control-Allow-Origin": "*",
          "Access-Control-Allow-Methods": "GET,POST,OPTIONS",
          "Access-Control-Allow-Headers": "Content-Type, Authorization",
        },
      });
    }
    if (path === "/health" || path === "/") {
      return json({ ok: true, service: "ventasplus-workers" });
    }
    if (!env.STORE) {
      return json({ ok: false, error: "STORE binding missing" }, 503);
    }
    const id = env.STORE.idFromName("ventasplus-main");
    const stub = env.STORE.get(id);
    return stub.fetch(request);
  },
};

export class VentasPlusStore {
  constructor(state, env) {
    this.state = state;
    this.env = env;
  }

  async fetch(request) {
    const url = new URL(request.url);
    const path = url.pathname;
    const method = request.method;

    try {
      if (path === "/api/publications" && method === "POST") {
        const data = await request.json();
        const id = crypto.randomUUID();
        const publication = {
          id,
          agentId: data.agentId || "agent-default",
          products: data.products || [],
          destinations: data.destinations || {},
          timestamp: data.timestamp || new Date().toISOString(),
          total: data.total || (data.products || []).length,
          facebook: data.facebook || [],
          whatsapp: data.whatsapp || [],
          status: "recorded",
          created_at: new Date().toISOString(),
        };
        await this.state.storage.put("pub:" + id, publication);
        const idxKey = "agent:" + publication.agentId + ":pubs";
        let idx = (await this.state.storage.get(idxKey)) || [];
        idx = [id, ...idx].slice(0, 200);
        await this.state.storage.put(idxKey, idx);
        return json({ ok: true, ...publication });
      }

      if (path.startsWith("/api/publications/agent/") && method === "GET") {
        const agentId = path.split("/").pop();
        const idx = (await this.state.storage.get("agent:" + agentId + ":pubs")) || [];
        const pubs = [];
        for (const id of idx) {
          const p = await this.state.storage.get("pub:" + id);
          if (p) pubs.push(p);
        }
        return json({ ok: true, agentId, publications: pubs });
      }

      if (path === "/api/agents" && method === "POST") {
        const data = await request.json();
        const id = data.id || crypto.randomUUID();
        const agent = {
          id,
          name: data.name || "Usuario",
          email: data.email || "",
          facebook_token: data.facebook_token || "",
          whatsapp_session_id: data.whatsapp_session_id || "",
          created_at: new Date().toISOString(),
          active: true,
        };
        await this.state.storage.put("agent:" + id, agent);
        return json({ ok: true, ...agent });
      }

      if (path.startsWith("/api/agents/") && method === "GET") {
        const id = path.split("/").pop();
        const agent = await this.state.storage.get("agent:" + id);
        if (!agent) return json({ ok: false, error: "not found" }, 404);
        return json({ ok: true, ...agent });
      }

      if (path === "/api/destinations" && method === "POST") {
        const data = await request.json();
        const id = crypto.randomUUID();
        const dest = {
          id,
          agentId: data.agentId || "agent-default",
          type: data.type || "whatsapp",
          externalId: data.externalId || "",
          name: data.name || "",
          active: true,
          created_at: new Date().toISOString(),
        };
        await this.state.storage.put("dest:" + id, dest);
        const idxKey = "agent:" + dest.agentId + ":dests";
        let idx = (await this.state.storage.get(idxKey)) || [];
        idx = [id, ...idx].slice(0, 200);
        await this.state.storage.put(idxKey, idx);
        return json({ ok: true, ...dest });
      }

      if (path.startsWith("/api/destinations/agent/") && method === "GET") {
        const agentId = path.split("/").pop();
        const idx = (await this.state.storage.get("agent:" + agentId + ":dests")) || [];
        const list = [];
        for (const id of idx) {
          const d = await this.state.storage.get("dest:" + id);
          if (d) list.push(d);
        }
        return json({ ok: true, agentId, destinations: list });
      }

      return json({ ok: false, error: "not found" }, 404);
    } catch (e) {
      return json({ ok: false, error: String(e) }, 500);
    }
  }
}

function json(obj, status = 200) {
  return new Response(JSON.stringify(obj), {
    status,
    headers: {
      "Content-Type": "application/json",
      "Access-Control-Allow-Origin": "*",
    },
  });
}
