//go:build !unix

package daemon

import "context"

func (s *Server) watchSignals(ctx context.Context) {
	<-ctx.Done()
}
