package render

import "list"

// teams/<team>.yml: a GitHub team, used only for CODEOWNERS (code review).
// UI access is by email (owners.yml), never by IdP groups (ADR 0011).
#Team: {
	githubTeam: =~"^[a-z0-9-]+/[a-z0-9._-]+$" | error("expected <org>/<team>, e.g. acme/payments-core")
}
teams: [#Name]: #Team

// Platform-owned: emails are matched against the OIDC `email` claim; only this domain is allowed.
_emailDomain: "example.com"
#Email:       =~"^[a-z0-9._%+-]+@example\\.com$" | error("expected a lowercase @\(_emailDomain) email")

#TeamRef: [if generic {#Name}, or(_teamNames) | error("unknown team; allowed: \(_teamList)")][0]

// stage -> emails that may promote into it. Unknown stage names get a custom error.
#Promoters: {
	if generic {[#Name]: [...#Email]}
	if !generic {
		for n in _stageNames {(n)?: [...#Email]}
		[_notStage]: error("unknown stage; allowed: \(_stageList)")
	}
	if generic {
		// editor schema: minItems/uniqueItems
		[#Name]: [_, ...] & list.UniqueItems()
	}
}

// per-app overrides: each field set replaces the capability's (promoters: per stage)
#AppOwners: {
	codeOwners?: [...#TeamRef]
	promoters?: #Promoters
	viewers?: [...#Email]
	if !generic {
		breakGlass?: error("breakGlass is capability-wide; set it at the top level")
	}
	if generic {
		codeOwners?: [_, ...] & list.UniqueItems()
	}
}

// capabilities/<cap>/owners.yml (required: codeOwners + promoters). One file per capability; apps
// override codeOwners / promoters stages / viewers under `apps:` (ADR 0023).
#Owners: {
	// GitHub teams (teams/): approve the app's code changes (CODEOWNERS)
	codeOwners?: [...#TeamRef]
	// stage -> emails: promote the app into that stage (and approve its Freight); can also view
	promoters?: #Promoters
	// emails: read-only view of the capability in Kargo and Argo CD
	viewers?: [...#Email]
	// emails: break-glass. Can approve Freight that skipped upstream verification (Kargo's "Manual
	// approval required" popup), then promote it. Reserved for critical promotions (ADR 0013).
	breakGlass?: [...#Email]
	// per-app overrides of codeOwners / promoters
	apps?: {
		if generic {[#Name]: #AppOwners}
		if !generic {
			for n in _appNames {(n)?: #AppOwners}
			[_notApp]: error("unknown app; allowed: \(_appList)")
		}
	}
	if !generic {
		owners?: error("renamed to codeOwners")
	}
	if generic {
		// editor schema: minItems/uniqueItems
		codeOwners?: [_, ...] & list.UniqueItems()
		viewers?:    list.UniqueItems()
		breakGlass?: list.UniqueItems()
	}
}

// Custom messages, computed from the raw YAML: conditions on the checked field itself would cycle,
// and a disjunction on the whole list would hide element errors like "unknown team". Written inline
// (not via a helper struct) so the error lands on the owners.yml field and lint can locate it.
_ownerWhat: {codeOwners: "team", viewers: "email", breakGlass: "email"}
_ownerOptional: {viewers: _, breakGlass: _} // may be absent or empty
capOwners: [...#Owners]
for o in _capOwnerFiles {capOwners: [{
	if o.codeOwners == _|_ {codeOwners: error("capability owners.yml needs codeOwners: [team, ..]")}
	if o.promoters == _|_ {promoters: error("capability owners.yml needs promoters: {<stage>: [email, ..]}")}
	for fld, w in _ownerWhat if o[fld] != _|_ {
		if _ownerOptional[fld] == _|_ if len(o[fld]) == 0 {(fld): error("needs at least one \(w)")}
		if len({for x in o[fld] {"\(x)": _}}) != len(o[fld]) {(fld): error("\(w)s must not repeat")}
	}
	if o.promoters != _|_ {
		if (o.promoters & [...]) != _|_ {promoters: error("expected a map: <stage>: [email, ..]")}
		if (o.promoters & [...]) == _|_ {
			promoters: {for sn, l in o.promoters {
				if len(l) == 0 {(sn): error("needs at least one email")}
				if len({for x in l {"\(x)": _}}) != len(l) {(sn): error("emails must not repeat")}
			}}
			// every stage an app uses needs a promoter, after the per-app merge
			for an, a in _capApps for sn, _ in a.stages if o.promoters[sn] == _|_ if o.apps[an].promoters[sn] == _|_ {
				promoters: error("app \(an) uses stage \(sn): needs promoters.\(sn) (or apps.\(an).promoters.\(sn))")
			}
		}
	}
	if o.apps != _|_ {
		apps: {for an, ao in o.apps {(an): {
			if ao.codeOwners != _|_ {
				if len(ao.codeOwners) == 0 {codeOwners: error("needs at least one team")}
				if len({for x in ao.codeOwners {"\(x)": _}}) != len(ao.codeOwners) {codeOwners: error("teams must not repeat")}
			}
			if ao.viewers != _|_ {
				if len({for x in ao.viewers {"\(x)": _}}) != len(ao.viewers) {viewers: error("emails must not repeat")}
			}
			if ao.promoters != _|_ {
				if (ao.promoters & [...]) != _|_ {promoters: error("expected a map: <stage>: [email, ..]")}
				if (ao.promoters & [...]) == _|_ {promoters: {for sn, l in ao.promoters {
					if len(l) == 0 {(sn): error("needs at least one email")}
					if len({for x in l {"\(x)": _}}) != len(l) {(sn): error("emails must not repeat")}
					if _capApps[an] != _|_ if _capApps[an].stages[sn] == _|_ if list.Contains(_stageNames, sn) {(sn): error("app \(an) doesn't use stage \(sn)")}
				}}}
			}
		}}}
	}
}]}
if !generic if len(_capOwnerFiles) == 0 {capOwners: error("capability needs owners.yml")}

_uniqSorted: {#in: [...string], out: list.SortStrings([for x, _ in {for x in #in {(x): _}} {x}])}

// the capability's owners.yml (empty struct when missing; capOwners reports that)
_o: {for o in capOwners {o}}

// Per app, sorted: the app's field if set, else the capability's.
codeOwners: {for an, _ in _capApps {(an): (_uniqSorted & {#in: [if _o.apps[an].codeOwners != _|_ {_o.apps[an].codeOwners}, _o.codeOwners][0]}).out}}
// per app, per stage the app uses (a stage in apps/<a>/app.yml `stages`)
_promoters: {for an, a in _capApps {(an): {for sn, _ in a.stages {
	if _o.apps[an].promoters[sn] != _|_ {(sn): (_uniqSorted & {#in: _o.apps[an].promoters[sn]}).out}
	if _o.apps[an].promoters[sn] == _|_ if _o.promoters[sn] != _|_ {(sn): (_uniqSorted & {#in: _o.promoters[sn]}).out}
}}}}
// capability-wide
_breakGlass: (_uniqSorted & {#in: [if _o.breakGlass != _|_ {_o.breakGlass}, []][0]}).out

// stages/ + targets/ approvers: union of the apps' codeOwners (capability codeOwners when there are no apps)
_allOwners: (_uniqSorted & {#in: [
	for _, c in codeOwners for t in c {t},
	if len(codeOwners) == 0 if _o.codeOwners != _|_ for t in _o.codeOwners {t},
]}).out
// everyone who can see the capability in Kargo / Argo CD: all promoters (every stage, app
// overrides too), viewers and break-glass
_allEmails: (_uniqSorted & {#in: [
	if _o.promoters != _|_ for _, l in _o.promoters for x in l {x},
	if _o.apps != _|_ for _, ao in _o.apps if ao.promoters != _|_ for _, l in ao.promoters for x in l {x},
	if _o.apps != _|_ for _, ao in _o.apps if ao.viewers != _|_ for x in ao.viewers {x},
	if _o.viewers != _|_ for x in _o.viewers {x},
	for x in _breakGlass {x},
]}).out
