# whodis

A Go service that resolves ownership for GitHub repositories and NAIS teams at NAV. It aggregates data from GitHub, Teamkatalogen, and NAIS Console.

## Endpoints

No authentication required.

### `GET /repository/{owner}/{repoName}/owners`

Returns a list of GitHub team slugs that own the repository. Admin teams are checked first; if none are found, all teams with access are returned. Returns `[]` if no teams have access.

```
GET /repository/navikt/appsec-guide/owners
```

```json
[
  "appsec",
  "nais"
]
```

---

### `GET /repository/{owner}/{repoName}/slackchannels`

Returns a list of Slack channels associated with teams that have access to the repository. Returns `[]` if no Slack channel could be found.

```
GET /repository/navikt/appsec-guide/slackchannels
```

```json
[
  "#appsec",
  "#nais-support"
]
```

---

### Repository identity and legacy routes

Use the GitHub repository's `full_name` directly in the path, for example `/repository/navikt/whodis/owners`. Only the `navikt` organization is currently supported. Organization names are case-insensitive and normalized to `navikt`; repository name casing is preserved.

Repository names must contain 1 to 100 ASCII letters, digits, dots, underscores or hyphens and cannot be `.` or `..`. Both repository endpoints return `400` with code `invalid_repository` for invalid identities or unsupported organizations.

The short-name routes `/repository/{repoName}/owners` and `/repository/{repoName}/slackchannels` are deprecated but remain available. They explicitly resolve repositories in `navikt` and use the same validation and resolution as the owner-qualified routes. No removal date has been set.

---

### `GET /ghuser/{username}`

Returns the NAV email address for a given GitHub username. Returns `404` if no email address is known for the user.

```
GET /ghuser/ola-nordmann
```

```json
{
  "email": "ola.nordmann@nav.no"
}
```

---

### `GET /nais/{teamSlug}`

Returns NAIS Console team details including members. Returns `404` if the team does not exist.

```
GET /nais/appsec
```

```json
{
  "slug": "appsec",
  "slackChannel": "#appsec",
  "purpose": "Application security at NAV",
  "members": [
    {
      "email": "ola.nordmann@nav.no",
      "name": "Ola Nordmann",
      "role": "MEMBER"
    }
  ]
}
```

---

### `GET /nais/{teamSlug}/repositories`

Returns a list of GitHub repositories owned by the given NAIS team. Repository names are in `nameWithOwner` format. Returns `404` if the team does not exist and `[]` if it has no repositories.

```
GET /nais/appsec/repositories
```

```json
[
  "navikt/repo-one",
  "navikt/repo-two"
]
```

---

## Responses

Successful responses are `200` with `Content-Type: application/json`. Lists are always JSON arrays, never `null`. An empty array means the resource exists but has no matching data.

Errors are [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem details with `Content-Type: application/problem+json`. `title` is the HTTP status phrase, and `code` is a stable, machine-readable identifier:

```json
{
  "type": "about:blank",
  "title": "Not Found",
  "status": 404,
  "detail": "The repository does not exist or is not accessible to whodis.",
  "instance": "/repository/navikt/does-not-exist/owners",
  "code": "repository_not_found"
}
```

| Status | `code` | Meaning |
|---|---|---|
| `400` | `invalid_repository`, `invalid_team_slug` | The path parameter is malformed or unsupported |
| `404` | `route_not_found` | No endpoint matches the request path |
| `404` | `repository_not_found` | GitHub cannot find the repository, or it is not accessible to whodis |
| `404` | `team_not_found` | The NAIS team does not exist |
| `404` | `user_not_found` | No email address is known for the GitHub user |
| `500` | `internal_error` | Unexpected error in whodis |
| `502` | `upstream_error` | GitHub, NAIS Console or Teamkatalogen failed |
| `504` | `upstream_timeout` | An upstream service did not respond in time |

The request log records the same value as `error.code`.

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
