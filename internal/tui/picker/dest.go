package picker

import (
	"context"
	"os"
	"path/filepath"

	"github.com/mistweaverco/syncsh/internal/repository"
	"github.com/mistweaverco/syncsh/internal/transport/directory"
)

type DestKind int

const (
	DestEmpty DestKind = iota
	DestUnrelated
	DestValidRepo
	DestPartialRepo
	DestUnsupported
	DestSyncshSubdirExists
)

type DestReport struct {
	Kind      DestKind
	Path      string
	Probe     repository.Report
	Subdir    string
	HasSubdir bool
}

func ClassifyDest(ctx context.Context, path string) (DestReport, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	tr := directory.New(abs)
	rep, err := repository.Probe(ctx, tr)
	if err != nil {
		return DestReport{}, err
	}
	out := DestReport{Path: abs, Probe: rep}
	switch rep.Result {
	case repository.Empty:
		out.Kind = DestEmpty
	case repository.Valid:
		out.Kind = DestValidRepo
	case repository.Partial:
		out.Kind = DestPartialRepo
	case repository.UnsupportedVersion:
		out.Kind = DestUnsupported
	default:
		out.Kind = DestUnrelated
		sub := filepath.Join(abs, "syncsh")
		if st, err := os.Stat(sub); err == nil && st.IsDir() {
			out.HasSubdir = true
			out.Subdir = sub
			subTr := directory.New(sub)
			subRep, err := repository.Probe(ctx, subTr)
			if err == nil && subRep.Result == repository.Valid {
				out.Kind = DestSyncshSubdirExists
				out.Probe = subRep
			}
		}
	}
	return out, nil
}

type DestChoice int

const (
	ChoiceUse DestChoice = iota
	ChoiceCreateSubfolder
	ChoiceSelectAnother
	ChoiceProceedAnyway
	ChoiceJoin
	ChoiceChooseSubfolderName
	ChoiceInspect
	ChoiceCancel
)

func ChoicesFor(kind DestKind) []DestChoice {
	switch kind {
	case DestEmpty:
		return []DestChoice{ChoiceUse, ChoiceSelectAnother, ChoiceCancel}
	case DestUnrelated:
		return []DestChoice{ChoiceCreateSubfolder, ChoiceSelectAnother, ChoiceProceedAnyway, ChoiceCancel}
	case DestSyncshSubdirExists:
		return []DestChoice{ChoiceUse, ChoiceSelectAnother, ChoiceChooseSubfolderName, ChoiceCancel}
	case DestValidRepo:
		return []DestChoice{ChoiceJoin, ChoiceSelectAnother, ChoiceCancel}
	case DestPartialRepo, DestUnsupported:
		return []DestChoice{ChoiceInspect, ChoiceSelectAnother, ChoiceCancel}
	default:
		return []DestChoice{ChoiceSelectAnother, ChoiceCancel}
	}
}
