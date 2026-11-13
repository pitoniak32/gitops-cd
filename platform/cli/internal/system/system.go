package system

import "os"

// Render validates one system under root and returns its rendered files, keyed by path under
// rendered/. Problems in the system's YAML come back as an Error (the diagnostics). teams: from
// LoadTeams, shared across systems (nil: read here); their own problems are Teams.Lint's.
func Render(cfg Config, root, sysName string, teams *Teams) (map[string]string, error) {
	m, diags, err := loadModel(cfg, root, sysName, teams)
	if err != nil {
		return nil, err
	}
	if len(diags) > 0 {
		return nil, Error(diags)
	}
	return m.files(), nil
}

// Lint validates one system under root (not the team files: Teams.Lint). The error is for I/O only.
func Lint(cfg Config, root, sysName string, teams *Teams) ([]Diag, error) {
	_, diags, err := loadModel(cfg, root, sysName, teams)
	return diags, err
}

// loadModel reads and validates a system. Diagnostics come in two rounds: what is wrong with the files
// as written, and only once those are clean, what shows in the values derived from them.
func loadModel(cfg Config, root, sysName string, teams *Teams) (*model, []Diag, error) {
	if r := os.Getenv("GITOPS_REPO"); r != "" {
		cfg.RepoURL = r
	}
	pre := map[string][]Check{}
	for et, checks := range cfg.PreDeploy {
		for _, c := range checks {
			n, err := c.Normalize()
			if err != nil {
				return nil, nil, err
			}
			pre[et] = append(pre[et], n)
		}
	}
	cfg.PreDeploy = pre
	if err := checkDir(root, sysName); err != nil {
		return nil, nil, err
	}
	if teams == nil {
		var err error
		if teams, err = LoadTeams(root); err != nil {
			return nil, nil, err
		}
	}
	in, diags, err := load(root, sysName, teams)
	if err != nil {
		return nil, nil, err
	}
	diags = append(diags, validate(cfg, in)...)
	if len(diags) > 0 {
		return nil, SortDedupe(diags), nil
	}
	m, diags := build(cfg, in)
	return m, SortDedupe(diags), nil
}
