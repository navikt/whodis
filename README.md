# whodis

A Go service that resolves ownership for GitHub repositories and NAIS teams at NAV. It aggregates data from GitHub, Teamkatalogen, and NAIS Console.

## Endpoints

Protected endpoints require a JWT Bearer token (RS256).

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `GET` | `/repository/{repoName}` | JWT | Returns admins of a GitHub repo with work emails and Teamkatalogen teams |
| `GET` | `/nais/{teamSlug}` | JWT | Returns NAIS team details: slug, Slack channel, purpose, and members |

## Development

**Prerequisites:** Go 1.26+ (or use [mise](https://mise.jdx.dev/) — `mise install` will set up the correct version).

```sh
# Build
make build        # outputs binary to bin/whodis

# Test
make test         # runs go test ./...
```
## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Contact

For any questions, issues, or feature requests, please reach out to the AppSec team:
- Internal: Either our slack channel [#appsec](https://nav-it.slack.com/archives/C06P91VN27M) or contact a [team member](https://teamkatalogen.nav.no/team/02ed767d-ce01-49b5-9350-ee4c984fd78f) directly via slack/teams/mail.
- External: [Open GitHub Issue](https://github.com/navikt/appsec-guide/issues/new/choose)
