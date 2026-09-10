package rclone

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/dont-be-evil-company/remnix/internal/transport"
)

func TestLocalRoundTrip(t *testing.T) {
	dir := t.TempDir()
	tr, err := OpenLocal(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := tr.PutAtomic(ctx, "events/d1/a.bundle", bytes.NewReader([]byte("hello"))); err != nil {
		t.Fatal(err)
	}
	r, err := tr.Get(ctx, "events/d1/a.bundle")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, _ := io.ReadAll(r)
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
	objs, err := tr.List(ctx, "events")
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 {
		t.Fatalf("list %v", objs)
	}
	if err := tr.Mkdir(ctx, "acks"); err != nil {
		t.Fatal(err)
	}
	dirs, err := tr.ListDirs(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range dirs {
		if d == "events" || d == "acks" {
			found = true
		}
	}
	if !found {
		t.Fatalf("dirs %v", dirs)
	}
	st, err := tr.HealthCheck(ctx)
	if err != nil || st.State != transport.HealthOK {
		t.Fatalf("health %+v %v", st, err)
	}
	if err := tr.Remove(ctx, "events/d1/a.bundle"); err != nil {
		t.Fatal(err)
	}
	if err := tr.Remove(ctx, "acks/missing.ack"); err != nil {
		t.Fatal(err)
	}
}

func TestPutAtomicOverwrites(t *testing.T) {
	dir := t.TempDir()
	tr, err := OpenLocal(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := tr.PutAtomic(ctx, "acks/dev.ack", bytes.NewReader([]byte("old-frontier"))); err != nil {
		t.Fatal(err)
	}
	if err := tr.PutAtomic(ctx, "acks/dev.ack", bytes.NewReader([]byte("new-frontier"))); err != nil {
		t.Fatal(err)
	}
	r, err := tr.Get(ctx, "acks/dev.ack")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, _ := io.ReadAll(r)
	if string(got) != "new-frontier" {
		t.Fatalf("got %q", got)
	}
	objs, err := tr.List(ctx, "acks")
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 {
		t.Fatalf("duplicate ack objects: %v", objs)
	}
}

func TestVersionPinned(t *testing.T) {
	if Version() == "" {
		t.Fatal("empty rclone version")
	}
}

func TestFsSpecTrailingSlashForBucketBackends(t *testing.T) {
	if got := fsSpec("remnix-s3", "dont-be-evil-company/remnix", "s3"); got != "remnix-s3:dont-be-evil-company/remnix/" {
		t.Fatalf("s3 spec: %q", got)
	}
	if got := fsSpec("remnix-s3", "my-bucket", "s3"); got != "remnix-s3:my-bucket/" {
		t.Fatalf("s3 bucket spec: %q", got)
	}
	if got := fsSpec("remnix-s3", "", "s3"); got != "remnix-s3:" {
		t.Fatalf("s3 empty spec: %q", got)
	}
	if got := fsSpec("gdrive", "remnix", "drive"); got != "gdrive:remnix" {
		t.Fatalf("drive must not get a trailing slash: %q", got)
	}
}

func TestIsHeadObjectForbidden(t *testing.T) {
	err := fmt.Errorf("operation error S3: HeadObject, https response error StatusCode: 403, api error Forbidden: Forbidden")
	if !isHeadObjectForbidden(err) {
		t.Fatal("expected HeadObject 403 to match")
	}
	if isHeadObjectForbidden(fmt.Errorf("operation error S3: ListObjectsV2, api error AccessDenied")) {
		t.Fatal("ListObjects denial is not the NewFs file probe")
	}
}

func TestFaultPartialWrite(t *testing.T) {
	dir := t.TempDir()
	base, err := OpenLocal(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	ft := &transport.FaultTransport{Base: base, FailWriteAfterBytes: 2}
	err = ft.PutAtomic(context.Background(), "metadata/manifest", bytes.NewReader([]byte("hello-world")))
	if err == nil {
		t.Fatal("expected truncated write error")
	}
}
