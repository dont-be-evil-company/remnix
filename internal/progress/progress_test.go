package progress

import (
	"context"
	"testing"
)

func TestReportNoopWithoutReporter(t *testing.T) {
	Report(context.Background(), "ignored")
}

func TestReportDelivers(t *testing.T) {
	var got []string
	ctx := With(context.Background(), func(msg string) {
		got = append(got, msg)
	})
	Report(ctx, "pulling events")
	Report(ctx, "")
	if len(got) != 1 || got[0] != "pulling events" {
		t.Fatalf("got %#v", got)
	}
}
