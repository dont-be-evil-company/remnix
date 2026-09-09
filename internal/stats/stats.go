package stats

import (
	"fmt"
	"io"

	"github.com/dont-be-evil-company/remnix/internal/history"
)

func Write(w io.Writer, st history.Stats) {
	fmt.Fprintf(w, "commands: %d\n", st.Commands)
	fmt.Fprintf(w, "deleted:  %d\n", st.Deleted)
	fmt.Fprintf(w, "success:  %d\n", st.Success)
	fmt.Fprintf(w, "failed:   %d\n", st.Failed)
	fmt.Fprintf(w, "devices:  %d\n", st.Devices)
	fmt.Fprintf(w, "sessions: %d\n", st.Sessions)
}
