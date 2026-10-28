# Stage 3 Spike — Venice Free Web Session

**Timebox:** 8 working hours
**Output:** a written verdict, no code
**Owner:** whoever runs the capture

---

## 1. Question

Can a logged-in `venice.ai/chat` browser session be driven by CPA, the way CPA drives Claude Code and Codex accounts today?

This is the only path to genuinely free Venice usage. The paid API has no free tier. If this spike fails, the project has no free route, and the README must say so.

---

## 2. What we are not doing

- We are not building a Cloudflare bypass.
- We are not automating account creation.
- We are not solving CAPTCHAs.
- We are not shipping anything before the verdict.

If the answer requires defeating an anti-bot control, the answer is no. Record that and stop.

---

## 3. Preconditions

1. A normal Venice account, logged in, in a desktop browser.
2. DevTools open on the Network tab, **Preserve log** on, **Disable cache** on.
3. A second window for notes.
4. One hour of quiet. The token lifetime test needs idle time.

---

## 4. Capture protocol

### Step 1 — Find the inference call

1. Open `https://venice.ai/chat`.
2. Send a short message: `ping`.
3. In Network, filter `Fetch/XHR`.
4. Find the request whose response contains the reply.

Record:

| Field | Value |
|---|---|
| Full URL | |
| Method | |
| Request headers of interest | |
| Request body shape | |
| Response content type | |
| Status | |

### Step 2 — Classify the auth

Look at how the request proves identity. One of:

- `Authorization: Bearer <jwt>` — good, easiest case
- a cookie such as `session=...` — workable, needs a cookie jar
- a custom header with a signature — hard, may bind to a nonce
- no auth header but a token in the body — unusual, record it

Record the header **name**, the value **shape** (JWT? opaque? length?), and whether it changes per request.

### Step 3 — Measure token lifetime

1. Copy the token value.
2. Wait 15 minutes without using the tab.
3. Send another message.
4. Compare the token.

Repeat at 1 hour and 24 hours if the session survives. Record the first moment it changes or fails.

### Step 4 — Look for request signing

Check for any of these, which make automation fragile:

- a header whose value is different on every request and is not a timestamp
- a `cf-` header tied to a challenge
- a body field containing a hash or nonce
- a websocket upgrade instead of plain HTTP

If present, stop and mark **blocked**.

### Step 5 — Measure limits

Send 20 short messages in a row. Record every `429` and the reset window. Note whether the limit is per minute, per hour, or per model.

### Step 6 — Check stream shape

Confirm whether the reply streams as SSE, and where the delta text lives. Compare with the OpenAI SSE format. A close match means translation is cheap. A custom frame means more work.

### Step 7 — Check tool and system support

Send one message with a system prompt. See whether it is honored or replaced. Try a tool call if the UI allows. Record what survives.

---

## 5. Record sheet

Copy this into the report and fill every row. Blank is a finding, not a pass.

| Item | Finding |
|---|---|
| Endpoint URL | |
| Method | |
| Auth mechanism | |
| Token type | |
| Token lifetime | |
| Refresh path | |
| Anti-bot controls | |
| Request signing | |
| Rate limit, observed | |
| Reset window | |
| Stream format | |
| System prompt honored | |
| Tools supported | |
| Model selector exposed in request | |
| Works from a plain HTTP client | yes / no |

---

## 6. Kill criteria

Stop and return **NO-GO** if any is true:

1. Inference needs a payload bound to a browser fingerprint or a solved challenge.
2. Token lifetime is under 10 minutes with no refresh path.
3. Rate limits make a coding agent impractical — for example under 20 requests per hour.
4. The endpoint refuses requests that do not originate from the web app origin.
5. Any step requires defeating Cloudflare Turnstile or similar.

A NO-GO is a successful spike. It saves a week of doomed work.

---

## 7. Report template

```markdown
# Stage 3 Verdict — Venice Web Session

Date:
Tester:
Account type: free / Pro
Browser:

## Verdict
GO / NO-GO

## One-paragraph reason

## Evidence
- endpoint:
- auth header name:
- token lifetime measured:
- rate limit measured:
- anti-bot controls seen:
- redacted sample request:
- redacted sample response frame:

## If GO — what a plugin would need
1.
2.
3.

## If NO-GO — what would change the answer
1.
```

Redaction rule: replace every token with `REDACTED`, keep the first four characters so shape is visible. Never paste a live cookie or JWT into the repo.

---

## 8. Decision after the spike

| Verdict | Action |
|---|---|
| GO | Open a new PRD stage with measured limits. Do not reuse the paid executor path blindly. |
| NO-GO | README states the free route does not exist. Close Stage 3. Keep stages 0–2. |

Either way, the verdict file lands in `docs/` and is linked from the README.

---

## 9. Why this is last

It has the worst ratio of payoff to risk. Stages 0 and 2 deliver working Venice access with known rules. Stage 3 trades certainty for a possible free tier. Do the certain work first.
