package cmd

import (
	"reflect"
	"testing"
)

func TestOpenURLs(t *testing.T) {
	want := []string{"https://kargo.localhost", "https://argocd.localhost/applications",
		"https://argocd-onprem.localhost/applications", "https://argocd-us.localhost/applications",
		"http://keycloak.localhost/admin/master/console/"}
	if got := openURLs([]string{"onprem", "us"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
}
