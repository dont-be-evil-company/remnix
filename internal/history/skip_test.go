package history

import "testing"

func TestShouldSkipRecoveryKey(t *testing.T) {
	if !ShouldSkip("SYNCSH_RECOVERY_KEY='syncsh1abc' syncsh unlock") {
		t.Fatal("recovery")
	}
	if !ShouldSkip("AWS_SECRET_ACCESS_KEY=x aws s3 ls") {
		t.Fatal("aws")
	}
}
