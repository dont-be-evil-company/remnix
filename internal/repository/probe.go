// Package repository classifies a syncsh remote without treating storage as trusted.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mistweaverco/syncsh/internal/crypto/generations"
	"github.com/mistweaverco/syncsh/internal/transport"
)

type Result int

const (
	Empty Result = iota
	Unrelated
	Valid
	Partial
	UnsupportedVersion
)

func (r Result) String() string {
	switch r {
	case Empty:
		return "empty"
	case Unrelated:
		return "unrelated"
	case Valid:
		return "valid"
	case Partial:
		return "partial"
	case UnsupportedVersion:
		return "unsupported_version"
	default:
		return "unknown"
	}
}

type Report struct {
	Result  Result
	Message string
}

const currentRemoteVersion = 1

type remoteManifest struct {
	Version          int    `json:"version"`
	ActiveGeneration string `json:"active_generation"`
}

var layoutPrefixes = []string{"metadata/", "keys/", "events/", "checkpoints/", "acks/"}

type shallowLister interface {
	ListShallow(ctx context.Context, prefix string) ([]transport.Object, error)
}

func listingExpensive(tr transport.Transport) bool {
	return tr.Capabilities().ListingExpensive
}

func Probe(ctx context.Context, tr transport.Transport) (Report, error) {
	if err := transport.PullSession(ctx, tr); err != nil {
		return Report{}, err
	}
	// Look up known layout keys first. Do not List("") - on Google Drive that
	// enumerates every file in the selected folder and hangs device add / join.
	manifestRaw, err := getOptional(ctx, tr, "metadata/manifest")
	if err != nil {
		return Report{}, err
	}
	gens, err := listGenerationManifests(ctx, tr)
	if err != nil {
		return Report{}, err
	}

	hasManifest := len(manifestRaw) > 0
	hasGens := len(gens) > 0

	if !hasManifest && !hasGens {
		looksLike, err := layoutPrefixExists(ctx, tr)
		if err != nil {
			return Report{}, err
		}
		if looksLike {
			return Report{Result: Partial, Message: "syncsh-looking files are present but metadata/manifest and keys/generations are missing"}, nil
		}
		if listingExpensive(tr) {
			return Report{Result: Empty, Message: "no syncsh metadata at this path"}, nil
		}
		dirs, err := tr.ListDirs(ctx, "")
		if err != nil {
			return Report{}, err
		}
		files, err := listShallow(ctx, tr, "")
		if err != nil {
			return Report{}, err
		}
		if hasLayoutDir(dirs) || looksLikeRepo(files, false, false) {
			return Report{Result: Partial, Message: "syncsh-looking files are present but metadata/manifest and keys/generations are missing"}, nil
		}
		if len(dirs) == 0 && len(files) == 0 {
			return Report{Result: Empty, Message: "no objects at this path"}, nil
		}
		return Report{Result: Unrelated, Message: "path contains files that are not a syncsh repository"}, nil
	}

	if hasManifest {
		var rm remoteManifest
		if err := json.Unmarshal(manifestRaw, &rm); err != nil {
			return Report{Result: Partial, Message: "metadata/manifest is not valid JSON"}, nil
		}
		ver := rm.Version
		if ver == 0 {
			ver = 1
		}
		if ver != currentRemoteVersion {
			return Report{Result: UnsupportedVersion, Message: fmt.Sprintf("unsupported repository version %d", rm.Version)}, nil
		}
		if rm.ActiveGeneration == "" {
			return Report{Result: Partial, Message: "metadata/manifest is missing active_generation"}, nil
		}
		if !hasGens {
			return Report{Result: Partial, Message: "keys/generations is missing or empty"}, nil
		}
		if !generationLooksValid(gens) {
			return Report{Result: Partial, Message: "keys/generations does not contain a valid generation manifest"}, nil
		}
		return Report{Result: Valid, Message: "existing syncsh repository"}, nil
	}

	return Report{Result: Partial, Message: "keys/generations found without metadata/manifest"}, nil
}

func looksLikeRepo(objs []transport.Object, hasManifest bool, hasGens bool) bool {
	if hasManifest || hasGens {
		return true
	}
	for _, o := range objs {
		for _, p := range layoutPrefixes {
			if strings.HasPrefix(o.Key, p) {
				return true
			}
		}
	}
	return false
}

func hasLayoutDir(dirs []string) bool {
	for _, d := range dirs {
		base := strings.Trim(d, "/")
		switch base {
		case "metadata", "keys", "events", "checkpoints", "acks":
			return true
		}
	}
	return false
}

func layoutPrefixExists(ctx context.Context, tr transport.Transport) (bool, error) {
	for _, p := range []string{"metadata", "keys", "events", "checkpoints", "acks"} {
		dirs, err := tr.ListDirs(ctx, p)
		if err != nil {
			return false, err
		}
		if len(dirs) > 0 {
			return true, nil
		}
		files, err := listShallow(ctx, tr, p)
		if err != nil {
			return false, err
		}
		if len(files) > 0 {
			return true, nil
		}
	}
	return false, nil
}

func listShallow(ctx context.Context, tr transport.Transport, prefix string) ([]transport.Object, error) {
	if s, ok := tr.(shallowLister); ok {
		return s.ListShallow(ctx, prefix)
	}
	return nil, nil
}

func listGenerationManifests(ctx context.Context, tr transport.Transport) ([][]byte, error) {
	dirs, err := tr.ListDirs(ctx, "keys/generations")
	if err != nil {
		return nil, err
	}
	var out [][]byte
	seen := map[string]bool{}
	for _, d := range dirs {
		key := strings.Trim(d, "/")
		if !strings.HasSuffix(key, "/manifest") {
			key = strings.TrimSuffix(key, "/") + "/manifest"
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		raw, err := getOptional(ctx, tr, key)
		if err != nil {
			return nil, err
		}
		if raw != nil {
			out = append(out, raw)
		}
	}
	files, err := listShallow(ctx, tr, "keys/generations")
	if err != nil {
		return nil, err
	}
	for _, o := range files {
		if !strings.HasSuffix(o.Key, "manifest") || seen[o.Key] {
			continue
		}
		seen[o.Key] = true
		raw, err := getOptional(ctx, tr, o.Key)
		if err != nil {
			return nil, err
		}
		if raw != nil {
			out = append(out, raw)
		}
	}
	return out, nil
}

func generationLooksValid(raws [][]byte) bool {
	for _, raw := range raws {
		var m generations.Manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		ver := m.Version
		if ver == 0 {
			ver = 1
		}
		if m.GenerationID != "" && ver == generations.CurrentVersion {
			return true
		}
	}
	return false
}

func getOptional(ctx context.Context, tr transport.Transport, key string) ([]byte, error) {
	r, err := tr.Get(ctx, key)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || os.IsNotExist(err) || errors.Is(err, transport.ErrRemoteNotFound) {
			return nil, nil
		}
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}
