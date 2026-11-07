package render

import (
	"list"
	"regexp"
)

// teams/<team>.yml: a GitHub team, used only for CODEOWNERS (code review).
// UI access is by email (owners.yml), never by IdP groups (ADR 0011).
#Team: {
	githubTeam: =~"^[a-z0-9-]+/[a-z0-9._-]+$" | error("expected <org>/<team>, e.g. acme/payments-core")
}
teams: [#Name]: #Team

// Platform-owned: emails are matched against the OIDC `email` claim; only this domain is allowed.
_emailDomain: "example.com"
#Email:       =~"^[a-z0-9._%+-]+@example\\.com$" | error("expected a lowercase @\(_emailDomain) email")

// capabilities/<cap>/owners.yml (required: owners + promoters) and apps/<app>/owners.yml
// (optional; each field it sets replaces the capability's).
#Owners: {
	// GitHub teams (teams/): approve the app's code changes (CODEOWNERS)
	owners?: [...[if generic {#Name}, or(_teamNames) | error("unknown team; allowed: \(_teamList)")][0]]
	// emails: promote the app to any stage (and approve Freight); can also view
	promoters?: [...#Email]
	// emails: read-only view of the capability in Kargo and Argo CD
	viewers?: [...#Email]
	if generic {
		// editor schema: minItems/uniqueItems
		owners?: [_, ...] & list.UniqueItems()
		promoters?: [_, ...] & list.UniqueItems()
		viewers?: list.UniqueItems()
	}
}

// Custom messages, computed from the raw YAML: conditions on the checked field itself would cycle,
// and a disjunction on the whole list would hide element errors like "unknown team". Written inline
// (not via a helper struct) so the error lands on the owners.yml field and lint can locate it.
_ownerWhat: {owners: "team", promoters: "email", viewers: "email"}
capOwners: [...#Owners]
for o in _capOwnerFiles {capOwners: [{for fld, w in _ownerWhat {
	if o[fld] == _|_ if fld != "viewers" {(fld): error("capability owners.yml needs \(fld): [\(w), ..]")}
	if o[fld] != _|_ {
		if fld != "viewers" if len(o[fld]) == 0 {(fld): error("needs at least one \(w)")}
		if len({for x in o[fld] {"\(x)": _}}) != len(o[fld]) {(fld): error("\(w)s must not repeat")}
	}
}}]}
if !generic if len(_capOwnerFiles) == 0 {capOwners: error("capability needs owners.yml")}
appOwners: [#Name]: #Owners
for an, o in {for k, v in in if k =~ _route.appOwners {(regexp.FindSubmatch(_route.appOwners, k)[1]): v}} {
	appOwners: (an): {for fld, w in _ownerWhat if o[fld] != _|_ {
		if fld != "viewers" if len(o[fld]) == 0 {(fld): error("needs at least one \(w)")}
		if len({for x in o[fld] {"\(x)": _}}) != len(o[fld]) {(fld): error("\(w)s must not repeat")}
	}}
}

_capField: {for f in ["owners", "promoters", "viewers"] {
	(f): [for o in capOwners if o[f] != _|_ for x in o[f] {x}]
}}
_uniqSorted: {#in: [...string], out: list.SortStrings([for x, _ in {for x in #in {(x): _}} {x}])}

// effective owners/promoters/viewers per app, sorted: the app's field if set, else the capability's
_effective: {for an, _ in apps {(an): {for f, capList in _capField {
	(f): (_uniqSorted & {#in: [if appOwners[an][f] != _|_ {appOwners[an][f]}, capList][0]}).out
}}}}
owners: {for an, e in _effective {(an): e.owners}}
_promoters: {for an, e in _effective {(an): e.promoters}}

// stages/ + targets/ approvers: union of the apps' owners (capability owners when there are no apps)
_allOwners: (_uniqSorted & {#in: [
	for _, o in owners for t in o {t},
	if len(owners) == 0 for t in _capField.owners {t},
]}).out
// everyone who can see the capability in Kargo / Argo CD: all promoters and viewers
_allEmails: (_uniqSorted & {#in: [
	for _, e in _effective for x in list.Concat([e.promoters, e.viewers]) {x},
	if len(_effective) == 0 for x in list.Concat([_capField.promoters, _capField.viewers]) {x},
]}).out
