# whodis

A Go service that resolves ownership for GitHub repositories and NAIS teams at NAV. It aggregates data from GitHub, Teamkatalogen, and NAIS Console.

## Endpoints

No authentication required.

### `GET /repository/{repoName}/owners`

Returns a list of GitHub team slugs that own the repository. Admin teams are checked first; if none are found, all teams with access are returned.

```
GET /repository/appsec-guide/owners
```

```json
[
  "appsec",
  "nais"
]
```

---

### `GET /repository/{repoName}/slackchannels`

Returns a list of Slack channels associated with teams that have access to the repository.

```
GET /repository/appsec-guide/slackchannels
```

```json
[
  "#appsec",
  "#nais-support"
]
```

---

### `GET /ghuser/{username}`

Returns the NAV email address for a given GitHub username.

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

Returns NAIS Console team details including members.

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

Returns a list of GitHub repositories owned by the given NAIS team. Repository names are in `nameWithOwner` format. Returns `404` if the team does not exist or has no repositories.

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

## Not-found responses

Unmatched request paths return HTTP `404` with an `application/problem+json` body explaining that no endpoint matches the path:

```json
{
  "type": "about:blank",
  "title": "Route not found",
  "status": 404,
  "detail": "No endpoint matches this request path.",
  "code": "route_not_found"
}
```

When GitHub cannot find a repository or it is not accessible to whodis, `/repository/{repoName}/owners` and `/repository/{repoName}/slackchannels` return HTTP `404` with an empty body instead.

The existing request log records `error.code` as `route_not_found` for unmatched paths or `resource_not_found` for GitHub repository-not-found responses, without a separate handler error log. Other not-found responses, such as a missing NAIS team or Slack channel, retain their existing behavior.

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
