# latexgrambot

Inline LaTeX bot for Telegram, written in Go. 

<!-- ai-notice:start -->
> [!IMPORTANT]
> **IMPORTANT NOTICE** This work has been made with an extra-supervised use of AI agents to perform the grunt work, with architectural choices strictly human-imposed. Has been successfully tested on a busy production environment, but in no case there is any guarantee nor I'm to be held liable of anything if you re-use this work.
<!-- ai-notice:end -->


## Features

- **Chat mode**: send a message containing LaTeX and the bot replies with a
  rich text message (the default). `/image` and `/pdf` return the PNG
  picture or the PDF document instead.
- **Inline mode**: in any chat, type `@yourbot <expression>` and pick one of
  the results — picture, PDF or rich text. Picking one sends the output into
  the chat immediately.
- **Rich text rendering**: replies are Telegram rich messages (Bot API 10.x
  `sendRichMessage`) whose `mathematical_expression` block is rendered
  natively by the client. Text formatting and the standard size declarations
  (`\large` through `\Huge`) are converted to native rich blocks; ambiguous
  bare input stays text by default. The raw source is never echoed.
- **Full LaTeX**: a broad default package set (amsmath, mathtools, tikz,
  pgfplots, siunitx, mhchem, chemfig, braket, hyperref, ...) is preloaded
  into a TeX format at image build time, so any of it is available without
  a custom preamble and without paying the package-load cost per render.
- **Per-user preamble and resolution**: `/setpreamble`, `/resetpreamble`,
  `/setdpi`.
- **Persistence and statistics**: per-chat settings, users, group
  memberships, render statistics and the inline file-id cache live in
  MongoDB; the public `/stats` command reports aggregates.
- **Observability**: OpenTelemetry traces over OTLP gRPC for the HTTP
  server, the Telegram API client, the MongoDB driver and every render.

## Architecture

```
                      Telegram
                         │  long polling (getUpdates)
                         ▼
             ┌───────────────────────┐
             │      latexgrambot     │
             │  ┌─────────────────┐  │
   commands  │  │   bot package   │  │
   messages ─┼─▶│ routing, inline │  │
   inline    │  │ settings, cache │  │
   queries   │  └────────┬────────┘  │
             │           │           │
             │  ┌────────▼────────┐  │      ┌──────────────┐
             │  │ render package  │──┼─────▶│   TeX Live   │
             │  │ pdflatex+poppler│  │      │  (in image)  │
             │  └────────┬────────┘  │      └──────────────┘
             │           │           │
             │  ┌────────▼────────┐  │      ┌──────────────┐
             │  │ store / files   │──┼─────▶│   MongoDB    │
             │  │ settings, stats │  │      └──────────────┘
             │  │ presigned URLs  │──┼─────▶┌──────────────┐
             │  └─────────────────┘  │      │ S3-compatible│
             └───────────┬───────────┘      │   bucket     │
                         │                  └──────────────┘
                         │  presigned GET URL fetched by Telegram
                         ▼
                    selected result
```

The bot is a single static Go binary plus the TeX Live/poppler runtime. It
only makes outbound connections: Telegram (long polling), MongoDB and the
S3-compatible bucket. No inbound traffic is required except when Telegram
fetches a presigned asset URL.

## How rendering works

1. The expression is validated (size caps, a small blocklist of file-reading
   TeX primitives) and, when it is entirely math without delimiters, wrapped
   in math mode up front.
2. `pdflatex` compiles the document with
   `-interaction=nonstopmode -halt-on-error -no-shell-escape` — shell escape
   is explicitly disabled for user-supplied code — under a hard timeout.
   When a compile fails only because math delimiters were missing, one retry
   wraps the whole expression in math mode; if that fails too, the original
   error is reported.
3. `pdftoppm` converts the first PDF page to a PNG or JPEG at the configured
   DPI. Small formulas rasterize to only a few hundred pixels, so the raster
   is re-rendered upscaled (smallest side 2560 px, capped at 4096): Telegram
   would otherwise upscale and JPEG-recompress the small picture, blurring
   the glyphs.
4. Only the formats a caller needs are produced: chat `/image` renders a
   PNG, chat `/pdf` skips rasterization entirely, inline answers render a
   JPEG plus the PDF.

The default preamble is preloaded into a TeX format (`LATEX_FORMAT`) at
image build time: a render compiles only the document body and loads no
packages.

## Inline delivery

Inline answers never send a temporary message to the querying user's chat.
If a normal `/image` or `/pdf` reply already produced a correctly typed
Telegram file ID for the same content hash, inline mode reuses it directly.
Otherwise the rendered JPEG/PDF is uploaded to the private bucket under a
fresh UUIDv4 object name and Telegram receives a short-lived presigned GET
URL, which it fetches when the result is picked. A background sweeper
deletes objects once their signed links have certainly expired (twice the
presign TTL, checked every 15 minutes). The native rich-text result needs
neither rendering nor storage.

Presigned URLs must be reachable from the public internet, so
`S3_PUBLIC_ENDPOINT_URL` has to be the public endpoint of your bucket. If
no S3 settings are provided, inline mode only offers the rich text result.

## Data model

MongoDB database (default `latexgram`):

| Collection | Contents |
|---|---|
| `settings` | per-chat preamble and DPI overrides |
| `users` | every interacting user, first/last seen |
| `groups` | chats the bot was added to or removed from |
| `stats` | one document per render: source, format, DPI, outcome, duration |
| `files` | Telegram file IDs captured from chat replies, per content hash |

S3 bucket layout: `<S3_PREFIX>/<uuidv4>.jpg` and `<S3_PREFIX>/<uuidv4>.pdf`.

## Requirements

- Go 1.23+ to build from source.
- `pdflatex` (TeX Live) and `pdftoppm` (poppler) at runtime; the provided
  Dockerfile installs everything, including the TeX collections backing the
  default preamble.
- MongoDB (only hard dependency).
- Optional: an S3-compatible bucket for inline picture/PDF results.
- A Telegram bot token. For rich text replies the bot must talk to a Bot
  API 10.x server (the default `api.telegram.org` qualifies).

## Quick start

### Docker

```sh
docker build -t latexgrambot .
docker run --rm -p 8080:8080 \
  -e TELEGRAM_BOT_TOKEN=123:abc \
  -e MONGODB_URI='mongodb://mongo:27017' \
  latexgrambot
```

### Docker Compose

```sh
TELEGRAM_BOT_TOKEN=123:abc docker compose up --build
```

This starts MongoDB and the bot. Add the S3 variables from the compose file
to enable inline picture/PDF results.

### From source

```sh
go build ./cmd/latexgrambot
TELEGRAM_BOT_TOKEN=123:abc MONGODB_URI='mongodb://localhost:27017' ./latexgrambot
```

`pdflatex` and `pdftoppm` must be on `PATH`. The preloaded format is only
present in the container image; local runs simply load the packages per
render.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `TELEGRAM_BOT_TOKEN` | required | Bot token (secret) |
| `MONGODB_URI` | required | MongoDB connection string |
| `MONGODB_DATABASE` | `latexgram` | Database with the collections above |
| `S3_ENDPOINT_URL` | empty | S3-compatible endpoint used by the bot |
| `S3_REGION` | `us-east-1` | S3 region used for signing |
| `S3_BUCKET` | empty | Bucket for inline assets |
| `S3_PREFIX` | `content` | Key prefix inside the bucket |
| `S3_PUBLIC_ENDPOINT_URL` | empty | Public endpoint used when signing GET URLs (must be reachable by Telegram) |
| `S3_PRESIGN_TTL` | `1h` | Lifetime of presigned download URLs |
| `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` | empty | S3 credentials (secret) |
| `LATEX_FORMAT` | empty | Preloaded TeX format name (the image ships `latexgrambot`); empty disables it |
| `RENDER_DPI` | `600` | Default picture resolution |
| `RENDER_TIMEOUT` | `30s` | Per-render `pdflatex` timeout |
| `RENDER_MAX_CONCURRENCY` | `2` | Simultaneous `pdflatex` runs |
| `MAX_EXPRESSION_LEN` | `4000` | Max expression length in characters |
| `MAX_PREAMBLE_LEN` | `4000` | Max preamble length in characters |
| `RICH_TEXT_ENABLED` | `true` | Rich text replies (needs a Bot API 10.x server) |
| `RICH_DEFAULT_MATH` | `false` | Parse ambiguous bare source as text; set `true` for legacy automatic bare-math detection |
| `HEALTH_ADDR` | `:8080` | Health endpoint address |
| `PDFLATEX_BIN` | `pdflatex` | pdflatex binary |
| `PDFTOPPM_BIN` | `pdftoppm` | pdftoppm binary |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | empty | OTLP gRPC collector; empty disables tracing |
| `OTEL_EXPORTER_OTLP_INSECURE` | `true` | Plaintext gRPC to the collector |
| `OTEL_SERVICE_NAME` | `latexgrambot` | Service name in traces |

## Deployment

### Kubernetes

The manifests in `k8s/` are a complete deployment for a single replica:

```sh
kubectl apply -f k8s/configmap.yaml
kubectl apply -f k8s/deployment.yaml
kubectl apply -f k8s/service.yaml
```

Edit `k8s/configmap.yaml` first (MongoDB URI, S3 settings) and create the
secrets from `k8s/secret.example.yaml`. The deployment runs as a non-root
user with a read-only capability set, drops all capabilities and ships
readiness/liveness probes against `/readyz` and `/healthz`.

Exactly one replica must run: Telegram delivers updates to a single
long-polling consumer per bot token, and the deployment uses the `Recreate`
strategy so an upgrade never runs two consumers at once.

### Argo CD

`argocd-application.yaml` is an optional Application manifest. Point
`repoURL` at your fork and adjust the project/namespace. Secrets are
deliberately not managed by Argo CD; apply them manually.

### Networking

No ingress is required for the bot itself. The only requirement is that
`S3_PUBLIC_ENDPOINT_URL` (when set) is reachable from the public internet,
because Telegram downloads the presigned asset URLs directly.

## Operations

### Health endpoints

- `GET /healthz` — process liveness, always `200` while the process runs.
- `GET /readyz` — `200` only when MongoDB answers and the Telegram polling
  loop has succeeded recently (a 409 conflict from a second consumer shows
  up here).

### Statistics

`/stats` (public) reports total renders split chat/inline, failed renders,
renders in the last 24 hours, users and active groups, aggregated from the
`stats`, `users` and `groups` collections.

### Observability

Set `OTEL_EXPORTER_OTLP_ENDPOINT` to export traces for the HTTP server, the
Telegram API client, the MongoDB driver and every render. Logs go to stdout
with `log.Printf`; failures of uploads, sweeps and Telegram calls are logged
with enough context to diagnose.

### Asset lifecycle

Inline assets are immutable, uniquely named and garbage collected by the
in-process sweeper: objects older than twice `S3_PRESIGN_TTL` (at least one
hour) are deleted every 15 minutes. Because names are unique, a later pick
of the same formula simply uploads a new object.

### Scaling and resource sizing

`pdflatex` is CPU-bound; each render also briefly uses a few hundred MB.
Tune `RENDER_MAX_CONCURRENCY` to the CPU available and keep the memory limit
comfortably above `concurrency × ~300MB`. The bot itself is lightweight;
MongoDB latency dominates the non-render part of a request, which is why
settings and file IDs are cached in memory.

### Backups

The only durable state is MongoDB (settings, users, groups, stats, file
IDs). Back up the `latexgram` database; the S3 bucket contents are
disposable.

### Upgrades

Deploy a new image and let the `Recreate` rollout finish. Renders in flight
are interrupted by design; the file-id cache is rebuilt automatically.

## Security

- Shell escape is disabled (`-no-shell-escape`) and file-reading primitives
  (`\input`, `\write`, `\read`, `\catcode`, ...) are rejected, so
  user-supplied LaTeX cannot read files or execute commands.
- Every compile runs with a hard timeout and a bounded number of concurrent
  runs; expression and preamble lengths are capped.
- The container runs as a non-root user with all capabilities dropped.
- Secrets (Telegram token, S3 credentials) are read from the environment
  and never logged.

## Development

```sh
go test ./...
go run ./cmd/latexgrambot
```

Unit tests run everywhere. Integration tests skip themselves unless the
environment is available:

- rendering tests need `pdflatex` and `pdftoppm` on `PATH`;
- MongoDB tests need `MONGODB_URI`;
- the S3 test needs `LATEXGRAMBOT_S3_TEST_ENDPOINT`,
  `LATEXGRAMBOT_S3_TEST_ACCESS_KEY` and `LATEXGRAMBOT_S3_TEST_SECRET_KEY`.

The GitHub Actions workflow (`.github/workflows/ci.yml`) runs the checks and
builds the multiarch image.

## License

BSD 3-Clause — see [LICENSE](LICENSE).
