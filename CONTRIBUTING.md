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
- The words in [GLOSSARY.md](GLOSSARY.md) have one meaning. Use them in code, output and docs.
- Program output, tool descriptions and docs are in English, in short and simple sentences.

## Commits and pull requests

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org): `feat: ...`, `fix: ...`, `docs: ...`.

A new feature that sends messages without the owner's approval, or that sends in bulk, does not fit zapgate.

## Releases

Versions follow [semver](https://semver.org) with a `v` prefix, the Go standard: `v0.1.0`, `v0.2.0`, `v0.2.1`. While the version is `0.x`:

- A change that breaks users bumps the minor version: a renamed variable, a changed tool input or output, or a database migration.
- A fix or a dependency update bumps the patch version. This includes whatsmeow updates.

To release, push a tag from an up-to-date `main`:

```bash
git tag v0.1.0 && git push origin v0.1.0
```

The release workflow publishes:

- the binaries for Linux and macOS, on amd64 and arm64, with `checksums.txt`, in a GitHub release with notes from the commits;
- the image `ghcr.io/ribeirogab/zapgate`, with the tags `0.1.0`, `0.1` and `latest`. A pre-release, such as `v0.2.0-rc.1`, does not get `latest`.

The version shows in the `serverInfo` of the MCP `initialize` response. A build without a tag shows `dev`.

Release at least once a month, so that whatsmeow stays current.

## Decisions

A decision that is hard to change goes into a record in [docs/adr](docs/adr/).
