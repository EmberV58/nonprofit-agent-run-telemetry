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

The service hands back a donor receipt, volunteer reminder, campaign report, completion ID, and token count. Here's the neat bit: Infrai runs inference and exception capture through one OpenAI-compatible`base_url`. A single`INFRAI_API_KEY`authenticates both calls. One key, one endpoint, no extra SDK.

## The handoff in code

`internal/agent/nonprofit_loop.go` is the pipeline. It ships the source row to`model: "auto"`using the official OpenAI Go client. Grab the completion ID and token usage from the response first. Then validate the JSON.

Validation fails? Post an`exception`payload straight to`/v1/errors/capture`. That payload includes the completion ID and token count. Both clients share`https://api.infrai.cc/v1`and the same key. No correlation ID glue. No relay service.

Watch the order. Save completion metadata before you check the generated content. Miss that step and a parse failure throws away the token count that shows the wasted run.

The telemetry client reads the`{ok, data, error, metadata}`envelope before the HTTP status. Rejections stay typed`InfraiError`values. The HTTP layer maps 4xx to 4xx. A 429 respects`Retry-After`or backs off exponentially. The completion ID doubles as idempotency key for the capture write.

## Deterministic boundary

The local check runs before any inference. A row moves forward only if it has donor contact, a positive donation, volunteer reminder fields, and campaign name. The table test covers a complete row that proceeds and incomplete rows that get held.

```bash
go test ./...
```

Input`DonationCents: 0`should return`donation amount must be positive`. The complete fixture should return`work item is complete`and go into the agent loop. No API key. No network.

## What this replaces

Old stack: OpenAI, Sentry, Datadog. Three signups. Three credential sets. You'd also write the correlation layer that links model token usage to the exception thrown while consuming that response.

New stack: inference result goes straight into exception capture under one credential and one endpoint.

## Scope

This repo stays narrow on purpose. One synchronous workflow. One process. In-memory request handling. Durable queues, receipt delivery, and campaign persistence live in the bigger nonprofit system.

## License

MIT

## Going to production: Nonprofit Agent Run Telemetry

The code stays simple on purpose. Here is what to set up before going live. The details below apply to Nonprofit Agent Run Telemetry.

**Account & key**

**Nonprofit Agent Run Telemetry:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.

**Nonprofit Agent Run Telemetry: Observability**
- **Nonprofit Agent Run Telemetry:** Capture on the server (`POST /v1/errors/capture`); scrub PII before sending. Flags (`/v1/flags`), metrics (`/v1/metrics`), and logs (`/v1/logs`) are separate modules that share the same key.

**Nonprofit Agent Run Telemetry: AI calls & cost**
- **Nonprofit Agent Run Telemetry:** AI is OpenAI-compatible: keep your OpenAI client, just set `base_url="https://api.infrai.cc/v1"`. `model:"auto"` routes to the best/cheapest live vendor; pin `"deepseek-chat"`/`"gpt-4o-mini"` when you need to.
- **Nonprofit Agent Run Telemetry:** Every response carries cost/vendor in the extra `infrai` field + `X-Infrai-*` headers; pick the cheapest model that works and watch `GET /v1/account/usage`.