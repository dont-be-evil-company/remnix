package history

import "testing"

func TestShouldSkipRecoveryKey(t *testing.T) {
	if !ShouldSkip("REMNIX_RECOVERY_KEY='remnix1abc' remnix unlock") {
		t.Fatal("recovery")
	}
	if !ShouldSkip("AWS_SECRET_ACCESS_KEY=x aws s3 ls") {
		t.Fatal("aws")
	}
}
