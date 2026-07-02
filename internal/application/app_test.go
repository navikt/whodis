package application

import (
	"os"
	"reflect"
	"testing"
)

func TestEnvVarsArePutInTheCorrectConfigProp(t *testing.T) {
	_ = os.Setenv("GITHUB_APP_PRIVATE_KEY", "pk")
	_ = os.Setenv("GITHUB_APP_CLIENT_ID", "cid")
	_ = os.Setenv("GITHUB_APP_INSTALLATION_ID", "aid")
	_ = os.Setenv("NAIS_SERVICE_ACCOUNT_TOKEN_PATH", "thepath")
	_ = os.Setenv("GITHUB_TEAMS_TO_SKIP", "first,second")

	expected := &config{
		AppPrivateKeyPem:   "pk",
		AppClientID:        "cid",
		AppInstallationID:  "aid",
		NaisApiKeyLocation: "thepath",
		GitHubTeamsToSkip:  []string{"first", "second"},
	}
	actual, err := configFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("Env vars end up in the wrong config prop: %v", actual)
	}
}
