```bash
> what is this?

 url health checker utility.
 local cli + plain-text http output.
 built for terminal and curl workflows.

> features?

 ✓ concurrent url checks with timeout control
 ✓ file-based checks for repeatable runs
 ✓ plain-text http mode
 ✓ no credentials, no database, no external accounts

> usage?

 alive check <url> [url...] [timeoutms]
 alive file <path> [timeoutms]
 alive serve [port] [timeoutms]

> examples?

 go run ./cmd/alive check https://example.com
 go run ./cmd/alive file targets.txt 2000
 go run ./cmd/alive serve 4177 2500
 curl "http://127.0.0.1:4177/check?url=https://example.com&url=https://go.dev"

> stack?

 go 1.26 stdlib

> run?

 go run ./cmd/alive check https://example.com
 go run ./cmd/alive serve 4177

> test?

 go test ./...

> proof?

 $ go test ./...
 ? github.com/keypad/alive/cmd/alive [no test files]

 $ go run ./cmd/alive check https://example.com 2500
 target state code latency size note
 https://example.com up 200 50ms - -

 $ curl "http://127.0.0.1:4177/check?url=https://example.com&url=https://go.dev"
 target state code latency size note
 https://example.com up 200 38ms - -
 https://go.dev up 200 208ms - -

> links?

 https://github.com/keypad/alive
```
