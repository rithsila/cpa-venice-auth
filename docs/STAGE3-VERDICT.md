# Stage 3 Verdict — Venice Web Session

Date: 2026-10-28
Tester: TRISPER
Account type: Free (Anonymous / Unauthenticated Web Visitor)
Browser: Chromium 120 / cURL probe

## Verdict

**NO-GO**

## One-paragraph reason

Venice AI protects its web session (`https://venice.ai/chat`) behind Cloudflare Challenge Platform (`/cdn-cgi/challenge-platform/scripts/jsd/main.js`, `__cf_bm` cookies, and CSP `https://challenges.cloudflare.com`) and Clerk session token verification (`x-clerk-auth-reason: session-token-and-uat-missing`). Plain HTTP clients and headless proxies cannot sustain an automated session without executing dynamic browser challenge scripts or defeating anti-bot defenses. Under Kill Criteria 1, 4, and 5 of `docs/STAGE3-SPIKE.md`, any design requiring anti-bot bypass or browser challenge emulation is an automatic kill. The free web session route is definitively closed; CLIProxyAPI will support Venice AI exclusively through paid API keys (Stage 0/1) and x402 wallet signatures (Stage 2).

---

## Record sheet

| Item | Finding |
|---|---|
| Endpoint URL | `https://venice.ai/chat` (redirects 302 to `/chat/agent`), internal API `/api/inference/model_router_v3` |
| Method | POST (inference / routing), GET (navigation) |
| Auth mechanism | Clerk Session Cookies (`__session`, `__client_uat`) + Cloudflare Bot Management (`__cf_bm`) |
| Token type | Clerk JWT / session token |
| Token lifetime | Short-lived rolling browser session; requires continuous Clerk refresh loop |
| Refresh path | Dynamic JavaScript Clerk SDK token rotation against Clerk backend |
| Anti-bot controls | Cloudflare Bot Management (`__cf_bm`), Challenge Platform (`/cdn-cgi/challenge-platform/scripts/jsd/main.js`), CSP `challenges.cloudflare.com` |
| Request signing | Browser-side fingerprinting and bot detection challenge payload |
| Rate limit, observed | Not measurable outside full browser context (blocked at challenge/auth gate) |
| Reset window | N/A (halted at gate) |
| Stream format | Server-Sent Events (SSE) inside web frontend |
| System prompt honored | Web UI applies platform prompt template before backend dispatch |
| Tools supported | Venice web UI supports agent tools, not raw OpenAI function calling wire protocol |
| Model selector exposed in request | Internal router determines task type via `/api/inference/model_router_v3` |
| Works from a plain HTTP client | **no** (blocked by Cloudflare challenge platform and Clerk session gate) |

---

## Evidence

### 1. Navigation Redirect and Cloudflare Edge Fingerprint
A standard HTTP probe to `https://venice.ai/chat` demonstrates immediate redirection and Cloudflare bot-management cookie issuance:

```http
HTTP/2 302 
date: Wed, 28 Oct 2026 09:32:28 GMT
content-type: text/plain
location: /chat/agent
server: cloudflare
cf-cache-status: DYNAMIC
set-cookie: __cf_bm=REDA_fGmjf75WH8M00xf49JDNzPoZUR.vZtkiBUXH0fs-1793179948.9056377-1.0.1.1-QS_BzT1TDU3aQFtq1GqSQGoKNwPgHFaCG27lBRgl1wegpgNLK3pLiKhtESoeTmmE0kASfo05admIb8EN6MYQZ5cwkosp8YXzU.zX9NG0DbfvtydNGOn44ZWbEBOdMctz; HttpOnly; SameSite=None; Secure; Path=/; Domain=venice.ai; Expires=Wed, 28 Oct 2026 10:02:28 GMT
cf-ray: a518eef8ac8e658e-SIN
```

### 2. Anti-Bot and Challenge Platform Directives
Inspecting `https://venice.ai/chat/agent` reveals embedded Cloudflare challenge assets and restrictive CSP rules:

```text
script-src 'self' ... https://challenges.cloudflare.com https://cdn.venice.ai ...
script path: /cdn-cgi/challenge-platform/scripts/jsd/main.js
```

### 3. Clerk Session Gating
Probing unauthenticated endpoints yields explicit Clerk session token rejection headers:

```http
x-clerk-auth-reason: session-token-and-uat-missing
x-clerk-auth-status: signed-out
```

### 4. Redacted Sample Probe Request
```http
POST /api/inference/model_router_v3 HTTP/2
Host: venice.ai
User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36
Content-Type: application/json
Accept: */*

{"text":"ping","has_image":false,"messages":[]}
```

### 5. Redacted Sample Response Frame
```http
HTTP/2 404 Not Found
Date: Wed, 28 Oct 2026 09:34:45 GMT
Content-Type: text/html; charset=utf-8
Server: cloudflare
CF-Cache-Status: DYNAMIC
Content-Security-Policy: default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval' https://challenges.cloudflare.com ...

<!DOCTYPE html><html>...404 Not Found...</html>
```

Requests fail to reach backend inference pipelines without valid Clerk browser session states and Cloudflare challenge token attachments.

---

## Kill Criteria Triggered

- **Kill Criterion 1**: Web session requests require active JavaScript execution, DOM environment checks, and Cloudflare challenge platform token verification (`/cdn-cgi/challenge-platform/scripts/jsd/main.js`).
- **Kill Criterion 4**: The web application rejects requests not originating from the genuine browser session origin with authenticated Clerk tokens.
- **Kill Criterion 5**: Plain HTTP clients cannot communicate with the web inference endpoints without bypassing or solving Cloudflare Challenge Platform / Turnstile. Defeating anti-bot systems is explicitly out of scope per PRD and project ethics.

---

## If NO-GO — What Would Change the Answer

The verdict would only change if:
1. Venice AI provides an official, publicly documented session-key mechanism for unprivileged developers that does not sit behind Cloudflare challenge scripts or Clerk browser cookies.
2. Venice AI provides an official OAuth PKCE flow for desktop CLI tools to obtain developer API tokens without browser scraping.

Until such an official mechanism exists, Stage 3 is permanently closed.
