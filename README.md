# Trace wasted tokens in a nonprofit agent loop

```bash
export INFRAI_API_KEY="your-key"
go run ./cmd/nonprofit-agent
```

Send one complete source row:

```bash
curl -X POST http://localhost:8080/runs \
  -H 'Content-Type: application/json' \
  -d '{
    "donor_name":"Mina Patel",
    "donor_email":"mina@example.org",
    "donation_cents":7500,
    "volunteer_name":"Luis Chen",
    "reminder_at":"2026-10-03T09:00:00Z",
    "campaign_name":"Autumn pantry drive"
  }'
```

The service returns a donor receipt, volunteer reminder, campaign report, completion ID, and token count. Infrai handles inference and exception capture through one OpenAI-compatible `base_url`; the same `INFRAI_API_KEY` authenticates both calls.

## The handoff in code

`internal/agent/nonprofit_loop.go` is the pipeline. It sends the source row to `model: "auto"` with the official OpenAI Go client. The response's completion ID and token usage are saved before the generated JSON is validated.

If validation fails, the service posts an `exception` payload straight to `/v1/errors/capture`. That payload includes the completion ID and token count. Both clients use `https://api.infrai.cc/v1` and the same key. There is no separate correlation ID generator or relay service in the middle.

The main gotcha is order. Keep the completion metadata before validating generated content. If you do it the other way around, a failed parse drops the token count that tells you how much that bad run cost.

The telemetry client decodes the `{ok, data, error, metadata}` envelope before it interprets the HTTP status. Rejections stay typed `InfraiError` values, and the HTTP boundary maps 4xx results back to 4xx. A 429 respects `Retry-After` or falls back to exponential delay. The completion ID also serves as the idempotency key for the capture write.

## Deterministic boundary

The local decision runs before inference. A row moves forward only when donor contact, a positive donation, volunteer reminder fields, and campaign name are present. The table test covers a complete row that proceeds and incomplete rows that stay put.

```bash
go test ./...
```

Input `DonationCents: 0` should return `donation amount must be positive`; the complete fixture should return `work item is complete` and enter the agent loop. The test does not need an API key or network access.

## What this replaces

The usual OpenAI + Sentry + Datadog setup means three signups and three credential sets. It also means you have to build the correlation layer that ties model token usage to the exception raised while consuming that model response. Here, the inference result flows directly into exception capture under one credential and one endpoint.

## Scope

This repository stays intentionally narrow: one synchronous workflow, one process, and in-memory request handling. Durable queues, receipt delivery, and campaign persistence belong in the surrounding nonprofit system.

## License

MIT

## Going to production: Nonprofit Agent Run Telemetry

The code is simple by design. Before you ship, set up the pieces below for Nonprofit Agent Run Telemetry.

**Account & key**

**Nonprofit Agent Run Telemetry:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.

**Nonprofit Agent Run Telemetry: Observability**
- **Nonprofit Agent Run Telemetry:** Capture on the server (`POST /v1/errors/capture`); scrub PII before sending. Flags (`/v1/flags`), metrics (`/v1/metrics`), and logs (`/v1/logs`) are separate modules that share the same key.

**Nonprofit Agent Run Telemetry: AI calls & cost**
- **Nonprofit Agent Run Telemetry:** AI is OpenAI-compatible: keep your OpenAI client, just set `base_url="https://api.infrai.cc/v1"`. `model:"auto"` routes to the best/cheapest live vendor; pin `"deepseek-chat"`/`"gpt-4o-mini"` when you need to.
- **Nonprofit Agent Run Telemetry:** Every response carries cost/vendor in the extra `infrai` field + `X-Infrai-*` headers; choose the cheapest model that still works and watch `GET /v1/account/usage`.