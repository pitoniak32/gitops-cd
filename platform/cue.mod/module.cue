module: "github.com/pitoniak32/gitops-cd@v0"
language: {
	version: "v0.17.0"
}
deps: {
	"cue.dev/x/k8s.io@v0": {
		v:       "v0.12.0"
		default: true
	}
}
