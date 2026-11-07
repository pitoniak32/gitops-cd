package render

import "list"

// teams/<team>.yml: who a team is. Platform-owned (CODEOWNERS).
#Team: {
	githubTeam: =~"^[a-z0-9-]+/[a-z0-9._-]+$" | error("expected <org>/<team>, e.g. acme/payments-core") // org/team, used for CODEOWNERS
	idpGroup:   =~"^[A-Za-z0-9._-]+$" | error("expected a group name: letters, digits, . _ -")          // OIDC group claim, used for Kargo promote RBAC
}
teams: [#Name]: #Team

// capabilities/<cap>/owners.yml (required) and apps/<app>/owners.yml (optional, replaces).
// Owners approve the app's code changes and promote it to any stage.
#Owners: {
	owners: [...[if generic {#Name}, or(_teamNames) | error("unknown team; allowed: \(_teamList)")][0]]
	if generic {owners: [_, ...] & list.UniqueItems()} // editor schema: minItems/uniqueItems
}

// custom messages; on instances only (an open list would trip them in the definition)
_ownerChecks: {
	owners: _
	if len(owners) == 0 {owners: error("needs at least one team; allowed: \(_teamList)")}
	if len({for o in owners {(o): _}}) != len(owners) {owners: error("teams must not repeat")}
}
capOwners: [...#Owners & _ownerChecks]
if !generic if len(_capOwnerFiles) == 0 {capOwners: error("capability needs owners.yml")}
appOwners: [#Name]: #Owners & _ownerChecks

_capOwnerList: [for o in capOwners for t in o.owners {t}]

// effective owners per app, sorted
owners: {for an, _ in apps {
	(an): list.SortStrings([for t, _ in {
		for t in [if appOwners[an] != _|_ {appOwners[an].owners}, _capOwnerList][0] {(t): _}
	} {t}])
}}
// stages/ + targets/ approvers: union of the apps' owners (capability owners when there are no apps)
_allOwners: list.SortStrings([for t, _ in {
	for _, o in owners for t in o {(t): _}
	if len(owners) == 0 for t in _capOwnerList {(t): _}
} {t}])
