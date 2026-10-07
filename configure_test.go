package main

import (
	"strings"
	"testing"

	"github.com/urfave/cli"
)

// USE_S3 without credentials stops at startup. common-services' NewS3Client
// returns nil without credentials, and the storage would dereference it on
// the first subtitle it stores. A run past the check fails to listen on the
// invalid WEB_HOST instead of serving.
func TestRunRefusesS3WithoutCredentials(t *testing.T) {
	t.Setenv("USE_S3", "true")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("USE_PROBE", "false")
	t.Setenv("WEB_HOST", "256.0.0.1")
	app := cli.NewApp()
	configure(app)
	err := app.Run([]string{"video-info"})
	if err == nil || !strings.Contains(err.Error(), "AWS_ACCESS_KEY_ID") {
		t.Fatalf("run: %v, want the missing-credentials error", err)
	}
}
