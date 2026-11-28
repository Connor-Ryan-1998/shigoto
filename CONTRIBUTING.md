# Contributing

Small, focused fixes and improvements welcome. Open an issue before starting a
large change.

1. Follow the [development guide](docs/development.md) to run locally.
2. Read [Interface standards](docs/ui-standards.md) before changing screens.
   Reuse existing components and API clients; preserve organisation boundaries,
   permissions, financial warnings and failed editor input.
3. Test behaviour in its owning layer using the [shared harnesses](docs/testing.md).
4. Run `uv run --locked go-task verify`, then `uv run --locked go-task audit`
   sequentially from a checkout inside the infrastructure repository. Standalone
   app checkouts use `go-task verify` and `go-task audit` directly.
5. Open a pull request describing the change and checks run. Include screenshots
   for visible changes.

Never use production databases for tests. Never commit credentials, session
cookies, sandbox password output or generated secrets.

Contributions are licensed under [Apache-2.0](LICENSE).
