# whodis

A Go service that resolves ownership for GitHub repositories and NAIS teams at NAV. It aggregates data from GitHub, Teamkatalogen, and NAIS Console.

## Endpoints

No authentication required.

### `GET /repository/{owner}/{repoName}/owners`

Returns a list of team slugs that own the repository. GitHub admin teams are checked first; if none are found, all GitHub teams with access are returned. If GitHub returns no eligible teams, Nais Console is checked for teams connected to the repository.

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

Returns a list of Slack channels associated with teams that have access to the repository. GitHub teams are resolved through Nais Console first. If no channels are found, teams connected to the repository in Nais Console are checked before the existing Teamkatalogen fallback. Empty channels are ignored and duplicate channels are removed. Returns `404` if no channel is found; upstream lookup failures return `500` rather than being treated as no matches.

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

Repository names must contain 1 to 100 ASCII letters, digits, dots, underscores or hyphens and cannot be `.` or `..`. Both repository endpoints return HTTP `400` with an `application/problem+json` body for invalid identities or unsupported organizations:

```json
{
  "type": "about:blank",
  "title": "Invalid repository",
  "status": 400,
  "detail": "only the navikt organization is supported",
  "code": "invalid_repository"
}
```

The short-name routes `/repository/{repoName}/owners` and `/repository/{repoName}/slackchannels` are deprecated but remain available. They explicitly resolve repositories in `navikt` and use the same validation and resolution as the owner-qualified routes. No removal date has been set.

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

When GitHub cannot find a repository or it is not accessible to whodis, both repository endpoints return HTTP `404` with an empty body instead. This applies to both owner-qualified and legacy routes.

The existing request log records `error.code` as `route_not_found` for unmatched paths or `resource_not_found` for GitHub repository-not-found responses, without a separate handler error log. Other not-found responses, such as a missing NAIS team or Slack channel, retain their existing behavior.

## Caching

Responses are built from upstream lookups that are cached in memory, so changes in GitHub, Nais Console or Teamkatalogen can take a while to show up. Each pod keeps its own cache, so two replicas may briefly disagree.

| Upstream lookup | Cached for | "Not found" or empty result cached for |
|---|---|---|
| GitHub teams with access to a repository | 15 min | 2 min |
| GitHub repositories a team has access to | 15 min | 2 min |
| Nais team details | 15 min | 2 min |
| Nais repositories of a team | 15 min | 2 min |
| Nais teams connected to a repository | 30 min | 2 min |
| Teamkatalogen membership by email | 30 min | empty memberships: 30 min; HTTP errors: not cached |

Concurrent identical lookups share one upstream request. Upstream failures (such as HTTP 5xx) are never cached. GitHub org users are loaded separately every 12 hours.

Cache behaviour is exposed on `/internal/metrics`: `whodis_cache_requests_total` (by `cache` and `result`: `hit`, `negative_hit`, `miss`), `whodis_cache_loads_total`, `whodis_cache_evictions_total` and `whodis_cache_entries`. Misses minus loads is the number of lookups that were coalesced into another request's load.

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
