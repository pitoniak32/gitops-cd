package capability

import "os"

// Render validates one capability under root and returns its rendered files, keyed by path under
// rendered/. Problems in the capability's YAML come back as an Error (the diagnostics).
func Render(cfg Config, root, capName string) (map[string]string, error) {
	m, diags, err := loadModel(cfg, root, capName)
	if err != nil {
		return nil, err
	}
	if len(diags) > 0 {
		return nil, Error(diags)
	}
	return m.files(), nil
}

// Lint validates one capability under root. The error is for I/O only.
func Lint(cfg Config, root, capName string) ([]Diag, error) {
	_, diags, err := loadModel(cfg, root, capName)
	return diags, err
}

// loadModel reads and validates a capability. Diagnostics come in two rounds: what is wrong with the files
// as written, and only once those are clean, what shows in the values derived from them.
func loadModel(cfg Config, root, capName string) (*model, []Diag, error) {
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
	if err := checkDir(root, capName); err != nil {
		return nil, nil, err
	}
	in, diags, err := load(root, capName)
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
