# Contributing

## Development

You need Go 1.26. `ffmpeg` is optional: without it, the voice note test skips the real conversion.

```bash
go test ./...
```

The tests run the whole server against a fake WhatsApp (`internal/whatsapp/fake`) and a fake transcoder. They need no phone and no network. The end-to-end tests are in `internal/app`, and the harness is in `internal/apptest`.

To run the server from source:

```bash
cd deploy && ./init-env.sh && docker compose up -d --build
```

## Code style

- Format with `gofmt`, and keep `go vet ./...` clean.
- No comments in code. Names and small functions explain the code.
- A change in behavior comes with a test in `internal/app` through the harness.
- The words in [CONTEXT.md](CONTEXT.md) have one meaning. Use them in code, output and docs.
- Program output, tool descriptions and docs are in English, in short and simple sentences.

## Commits and pull requests

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org): `feat: ...`, `fix: ...`, `docs: ...`.

A new feature that sends messages without the owner's approval, or that sends in bulk, does not fit zapgate.

## Decisions

A decision that is hard to change goes into a record in [docs/adr](docs/adr/).
