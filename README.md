# sk6

Zero-Code Scripting: 스웨거 명세(정적 구조)만 있으면 k6 스크립트를 직접 짤 필요가 없다.

Context-Aware E2E (with LLM): API 간의 의존성(토큰 체이닝 등)은 AI가 알아서 조립 설명서(JSON)를 그려준다.

Scale-Driven Testing: 100만 건, 1억 건 대용량 조회를 위한 랜덤 파라미터와 목표 TPS 제어를 커맨드 한 줄로 추상화한다.

## Quick Start

```bash
make build
./sk6 --spec ./openapi.yaml --tps 1 --scale 10M --output generated_script.js
k6 run generated_script.js
```

`--spec` accepts an OpenAPI 3.x or Swagger 2.0 file path or URL. Without an AI scenario prompt, sk6 defaults to public `GET`/`HEAD` operations only. The interactive run option mixes read operations with `POST`/`PUT`/`PATCH` command operations and still excludes `DELETE`; use `--allow-unsafe` only when you intentionally want every operation.

You can also keep the command short:

```bash
./sk6 ./openapi.yaml
./sk6 ./openapi.yaml --run
./sk6 ./openapi.yaml "login, create an order, then fetch the order" --run
```

If you run `./sk6` with no arguments in a terminal, sk6 asks only for the missing required inputs and uses defaults for the rest.

When you provide only a spec URL or file in an interactive terminal, sk6 parses the spec first and asks what to do:

```text
Choose what to do:
  1) Safe public read scenario (GET/HEAD only)
  2) Precise scenario from test evidence JSON
  3) Mixed read/command scenario
  4) Enter AI scenario prompt
  5) Allow unsafe static all operations
  6) Adjust run settings
  q) Cancel
```

After choosing a scenario type, sk6 asks whether to run k6 or only write the generated script.
The safe public read option still uses only unauthenticated `GET`/`HEAD` operations, but it reads OpenAPI response definitions and emits exact status checks when concrete 2xx responses are declared.
`Adjust run settings` shows the current `TPS`, `duration`, `scale`, `base-url`, and auth setup; choose the number for the setting you want to edit, then enter `done` to return to the main menu. Auth values are summarized without printing pasted bearer tokens.

The mixed read/command option excludes `DELETE`, but it can still create or update data through `POST`, `PUT`, or `PATCH`. Use it against disposable or load-test-safe data.

Use `--yes` to skip this menu and use flags/defaults directly.

## Run k6

Install k6, then let sk6 generate, run, export a JSON summary, and create an HTML report:

```bash
./sk6 \
  --spec ./openapi.yaml \
  --tps 1 \
  --scale 10M \
  --duration 1m \
  --run \
  --open-report
```

The generated script injects random path/query/body values from `--scale`, sends JSON request bodies when the spec defines `requestBody`, and uses `AUTH_TOKEN` for bearer auth placeholders when an operation declares security.

For auth-required scenarios, prefer injecting a token or a disposable test account instead of exercising the signup flow during load tests:

```bash
AUTH_TOKEN=... sk6 ./openapi.yaml "Fetch my dashboard with uneven traffic" --run
sk6 ./openapi.yaml "Fetch my dashboard with uneven traffic" --auth-token-file ./.token --run
sk6 ./openapi.yaml "Fetch my dashboard with uneven traffic" \
  --auth-login-path /api/v1/auth/login \
  --auth-username loadtest@example.com \
  --auth-password 'test-password' \
  --auth-token-json-path data.accessToken \
  --run
```

When running interactively, sk6 detects auth-required operations and asks whether to continue with existing `AUTH_TOKEN`, paste a token, read a token file, or log in with a test account. Tokens are passed to k6 as `AUTH_TOKEN`; they are not written into the generated script.

By default, each run writes artifacts into a timestamped folder:

```text
sk6-results/20260529-153000/
  generated_script.js
  k6-summary.json
  report.html
```

Use `--output-dir` to change the parent folder, or pass `--output`, `--summary`, and `--report` when you want exact paths.

For non-interactive static command coverage, use `--include-commands`. This includes `GET`/`HEAD`/`POST`/`PUT`/`PATCH`, and includes auth-required operations when auth is configured. It still excludes `DELETE`:

```bash
sk6 ./openapi.yaml --include-commands --auth-token-file ./.token --run --yes
```

Use `--allow-unsafe` only against disposable data when you intentionally want static mode to call every operation in the spec, including `DELETE`:

```bash
sk6 ./openapi.yaml --allow-unsafe --tps 1 --duration 10s --run
```

Generated artifacts are kept so you can inspect them. Add `--clean` to remove generated files after the command finishes:

```bash
sk6 ./openapi.yaml --run --clean
```

## AI Scenario Mapping

Pass a natural language scenario. By default, sk6 uses your authenticated Codex/OpenAI login through `codex exec`.

```bash
sk6 ./openapi.yaml "Create an order, extract the order id, then fetch it" --run
```

You can choose the AI provider explicitly:

```bash
sk6 ./openapi.yaml "Create an order, then fetch it" --ai-provider codex --run
OPENAI_API_KEY=... sk6 ./openapi.yaml "Create an order, then fetch it" --ai-provider openai-api --run
sk6 ./openapi.yaml "Create an order, then fetch it" --ai-provider auto --run
```

Provider behavior:

- `codex`: use authenticated Codex/OpenAI login with the account default model. ChatGPT-backed Codex accounts may reject explicit model overrides.
- `openai-api`: use `OPENAI_API_KEY` with `gpt-4o-mini` by default.
- `auto`: use `OPENAI_API_KEY` if set, otherwise Codex login.

Override the model when needed:

```bash
OPENAI_API_KEY=... sk6 ./openapi.yaml "Create an order, then fetch it" --ai-provider openai-api --model gpt-4o-mini --run
```

API key mode is available:

```bash
OPENAI_API_KEY=... sk6 ./openapi.yaml "Create an order, extract the order id, then fetch it" --ai-provider openai-api --run
```

## Test Evidence Scenarios

Use `--from-tests` with a JSON evidence file when you already know the exact API flow from tests or fixtures:

```bash
sk6 ./openapi.yaml --from-tests ./test-evidence.json --run
```

Evidence calls can provide `api_id` directly, or `method` and `path` to match an OpenAPI operation. Static path values are converted into path parameter overrides when they match templated paths such as `/orders/{id}`.

```json
{
  "name": "create order then fetch",
  "calls": [
    {
      "method": "POST",
      "path": "/orders",
      "body": { "sku": "A-001" },
      "expect_status": 201,
      "extract": { "orderId": "data.id" }
    },
    {
      "method": "GET",
      "path": "/orders/42",
      "use": { "id": "orderId" },
      "expect_status": 200
    }
  ]
}
```

Supported checks include `expect_status` and explicit `checks` entries with `type` values `status`, `json_path`, `header`, and `body_contains`.
