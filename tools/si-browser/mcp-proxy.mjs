#!/usr/bin/env node
/**
 * Tiny local reverse proxy to smooth over MCP endpoint path expectations.
 *
 * Problem:
 * - Some clients connect to `/mcp` with a GET expecting SSE (legacy transport).
 * - `@playwright/mcp` serves legacy SSE on `/sse` and streamable HTTP on `/mcp`.
 *
 * Solution:
 * - Rewrite only GET /mcp* -> /sse*.
 * - Proxy everything else as-is.
 *
 * Usage:
 *   node infra/playwright/mcp-proxy.mjs
 *
 * Env:
 *   PROXY_BIND=127.0.0.1
 *   PROXY_PORT=8931
 *   UPSTREAM_BASE=http://127.0.0.1:8932
 */

import http from "node:http";
import { request as httpRequest } from "node:http";
import { URL } from "node:url";

const bindHost = process.env.PROXY_BIND || "127.0.0.1";
const bindPort = Number(process.env.PROXY_PORT || "8931");
const upstreamBase = process.env.UPSTREAM_BASE || "http://127.0.0.1:8932";

// Codex's rmcp client can cache an `mcp-session-id` across runs. If the upstream
// server is restarted, it will return 404 "Session not found". We translate the
// client's stable session id to a live upstream session id.
const clientToUpstreamSession = new Map(); // clientSid -> Promise<string>

function firstHeaderValue(value) {
  if (!value) return "";
  if (Array.isArray(value)) return value[0] || "";
  return String(value);
}

function createUpstreamSession() {
  return new Promise((resolve, reject) => {
    const target = new URL("/mcp", upstreamBase);
    const body = JSON.stringify({
      jsonrpc: "2.0",
      id: 1,
      method: "initialize",
      params: {
        protocolVersion: "2024-11-05",
        capabilities: {},
        clientInfo: { name: "mcp-proxy", version: "0" },
      },
    });

    const req = httpRequest(
      target,
      {
        method: "POST",
        headers: {
          accept: "text/event-stream, application/json",
          "content-type": "application/json",
          "content-length": Buffer.byteLength(body).toString(),
          host: target.host,
        },
      },
      (res) => {
        const sid = firstHeaderValue(res.headers["mcp-session-id"]);
        if (!sid) {
          res.resume();
          reject(
            new Error(
              `upstream initialize missing mcp-session-id (status=${res.statusCode})`,
            ),
          );
          return;
        }

        // The initialize response is SSE but should complete quickly.
        // If it doesn't, treat it as a hard failure (session might not be committed).
        const timer = setTimeout(() => {
          req.destroy(new Error("upstream initialize timed out"));
        }, 8000);

        res.on("end", () => {
          clearTimeout(timer);
          resolve(sid);
        });
        res.on("error", (err) => {
          clearTimeout(timer);
          reject(err);
        });
        res.resume();
      },
    );

    req.on("error", reject);
    req.end(body);
  });
}

function getUpstreamSessionForClient(clientSid) {
  const existing = clientToUpstreamSession.get(clientSid);
  if (existing) return existing;

  const p = createUpstreamSession()
    .then((sid) => {
      // Store a resolved promise for subsequent callers.
      const resolved = Promise.resolve(sid);
      clientToUpstreamSession.set(clientSid, resolved);
      process.stdout.write(
        `[mcp-proxy] mapped client session ${clientSid} -> upstream session ${sid}\n`,
      );
      return sid;
    })
    .catch((err) => {
      clientToUpstreamSession.delete(clientSid);
      throw err;
    });

  clientToUpstreamSession.set(clientSid, p);
  return p;
}

function rewritePath(method, url) {
  if (method !== "GET") return url;
  if (!url) return url;

  // Some clients treat the configured URL as a base and probe `/` first.
  if (url === "/") return "/sse";

  // Rewrite only the Playwright MCP entrypoint path.
  // Examples:
  // - /mcp               -> /sse
  // - /mcp?foo=bar       -> /sse?foo=bar
  // - /mcp/path?x=y      -> /sse/path?x=y
  if (url === "/mcp") return "/sse";
  if (url.startsWith("/mcp?")) return "/sse" + url.slice("/mcp".length);
  if (url.startsWith("/mcp/")) return "/sse" + url.slice("/mcp".length);
  return url;
}

function readRequestBody(req, { sampleLimit = 1000, maxBytes = 1024 * 1024 } = {}) {
  return new Promise((resolve, reject) => {
    const chunks = [];
    let total = 0;
    let sampled = "";

    req.on("data", (chunk) => {
      total += chunk.length;
      if (total > maxBytes) {
        reject(new Error(`request body too large (> ${maxBytes} bytes)`));
        req.destroy();
        return;
      }
      chunks.push(chunk);
      if (sampled.length < sampleLimit) {
        sampled += chunk.toString("utf8").slice(0, sampleLimit - sampled.length);
      }
    });
    req.on("end", () => resolve({ body: Buffer.concat(chunks), sampled }));
    req.on("error", reject);
  });
}

const server = http.createServer(async (req, res) => {
  const method = req.method || "GET";
  const url = req.url || "/";

  // Minimal request logging to help debug client/protocol mismatches.
  const accept = req.headers.accept || "";
  const contentType = req.headers["content-type"] || "";
  const headersForLog = { ...req.headers };
  if (headersForLog.authorization) headersForLog.authorization = "<redacted>";
  if (headersForLog.cookie) headersForLog.cookie = "<redacted>";
  process.stdout.write(
    `[mcp-proxy] ${method} ${url} accept=${JSON.stringify(accept)} headers=${JSON.stringify(headersForLog)}\n`,
  );

  const targetPath = rewritePath(method, url);
  const target = new URL(targetPath, upstreamBase);

  const clientSid = firstHeaderValue(req.headers["mcp-session-id"]);

  let body = Buffer.alloc(0);
  let sampled = "";
  try {
    const read = await readRequestBody(req);
    body = read.body;
    sampled = read.sampled;
    if (sampled.length > 0 || contentType) {
      process.stdout.write(
        `[mcp-proxy] body-sample content-type=${JSON.stringify(contentType)} sample=${JSON.stringify(sampled)}\n`,
      );
    }
  } catch (err) {
    res.writeHead(413, { "content-type": "text/plain; charset=utf-8" });
    res.end(`mcp-proxy body error: ${err?.message || String(err)}\n`);
    return;
  }

  const headers = { ...req.headers };
  // Ensure Host header matches upstream to avoid host-check edge cases.
  headers.host = target.host;

  // Session translation: keep the client's session id stable while ensuring the
  // upstream sees a live session id that exists on the server.
  if (clientSid && url.startsWith("/mcp")) {
    try {
      const upstreamSid = await getUpstreamSessionForClient(clientSid);
      headers["mcp-session-id"] = upstreamSid;
      process.stdout.write(
        `[mcp-proxy] forward session client ${clientSid} -> upstream ${upstreamSid}\n`,
      );
    } catch (err) {
      res.writeHead(502, { "content-type": "text/plain; charset=utf-8" });
      res.end(`mcp-proxy init error: ${err?.message || String(err)}\n`);
      return;
    }
  }

  const sendUpstream = () =>
    new Promise((resolve, reject) => {
      const upstreamReq = httpRequest(
        target,
        {
          method,
          headers: {
            ...headers,
            "content-length": String(body.length),
          },
        },
        (upstreamRes) => {
          const upstreamCT = upstreamRes.headers["content-type"] || "";
          const upstreamSid = firstHeaderValue(upstreamRes.headers["mcp-session-id"]);
          if (clientSid && upstreamSid) {
            clientToUpstreamSession.set(clientSid, Promise.resolve(upstreamSid));
            process.stdout.write(
              `[mcp-proxy] refreshed mapping client ${clientSid} -> upstream ${upstreamSid}\n`,
            );
          }

          process.stdout.write(
            `[mcp-proxy] upstream ${method} ${target.pathname}${target.search} -> ${upstreamRes.statusCode} content-type=${JSON.stringify(upstreamCT)}\n`,
          );

          // If session is missing, retry once by re-initializing upstream.
          if ((upstreamRes.statusCode || 0) === 404 && clientSid) {
            const chunks = [];
            upstreamRes.on("data", (c) => chunks.push(c));
            upstreamRes.on("end", () => {
              const text = Buffer.concat(chunks).toString("utf8");
              process.stdout.write(
                `[mcp-proxy] upstream 404 body=${JSON.stringify(text)}\n`,
              );
              resolve({ retryable404: text.includes("Session not found") });
            });
            upstreamRes.on("error", reject);
            upstreamRes.resume();
            return;
          }

          const responseHeaders = { ...upstreamRes.headers };
          if (clientSid && responseHeaders["mcp-session-id"]) {
            responseHeaders["mcp-session-id"] = clientSid;
          }
          res.writeHead(upstreamRes.statusCode || 502, responseHeaders);
          upstreamRes.pipe(res);
          resolve({ streamed: true });
        },
      );

      upstreamReq.on("error", reject);
      upstreamReq.end(body);
    });

  let attempt = 0;
  while (attempt < 2) {
    try {
      const out = await sendUpstream();
      if (out?.streamed) return;
      if (out?.retryable404) {
        attempt += 1;
        clientToUpstreamSession.delete(clientSid);
        const upstreamSid = await getUpstreamSessionForClient(clientSid);
        headers["mcp-session-id"] = upstreamSid;
        process.stdout.write(
          `[mcp-proxy] retry with new upstream session ${upstreamSid}\n`,
        );
        continue;
      }
      // Non-retryable 404 should never reach here, but guard anyway.
      res.writeHead(502, { "content-type": "text/plain; charset=utf-8" });
      res.end("mcp-proxy error: upstream returned 404\n");
      return;
    } catch (err) {
      res.writeHead(502, { "content-type": "text/plain; charset=utf-8" });
      res.end(`mcp-proxy error: ${err?.message || String(err)}\n`);
      return;
    }
  }
});

server.listen(bindPort, bindHost, () => {
  // Intentionally minimal log for use in shell background.
  process.stdout.write(
    `mcp-proxy listening on http://${bindHost}:${bindPort} -> ${upstreamBase}\n`,
  );
});
