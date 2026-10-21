# Contributing to JiraStopWatch NextGen

Thanks for your interest! Bug reports, ideas, translations and pull requests are all welcome.

## Reporting a bug or suggesting a feature
Open an [issue](https://github.com/farhadrezvani7/jirastopwatch-nextgen/issues/new/choose) and pick the matching template. For bugs, please include your OS, the app version (profile window), whether you use Jira Cloud or Server/Data Center, and the steps to reproduce. Never paste API tokens or passwords.

## Building and testing
You need Go 1.24 or newer.

```bash
git clone https://github.com/farhadrezvani7/jirastopwatch-nextgen.git
cd jirastopwatch-nextgen
go build -o jirastopwatch-nextgen .
go test ./...
```

## Pull requests
1. Fork the repository and create a branch from `main`.
2. Keep each pull request focused on one change, and add or update tests when behaviour changes.
3. Make sure `go vet ./...` and `go test ./...` pass.
4. Describe what changed and why in the pull request.

## Good first issues
Issues labelled `good first issue` and `help wanted` are a good place to start. Translations of the UI and README are also very welcome.

## License
By contributing you agree that your contributions are licensed under the [Apache License 2.0](LICENSE).
