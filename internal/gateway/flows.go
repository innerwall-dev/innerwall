package gateway

import (
	"errors"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	innerwallv1 "github.com/innerwall-dev/innerwall/internal/gen/innerwall/v1"
	"github.com/innerwall-dev/innerwall/internal/ingest"
	"github.com/innerwall-dev/innerwall/internal/registry"
)

// ReportFlows implements the flow telemetry stream (ADR-0015): one
// request per aggregation window, ingested as it arrives, and the
// cumulative accepted count returned when the agent closes the stream. The
// reporting workload is the identity the interceptor derived from the
// connection credential; the payload names nobody.
func (s *Server) ReportFlows(stream innerwallv1.AgentService_ReportFlowsServer) error {
	ctx := stream.Context()
	id, ok := IdentityFromContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "workload credential required")
	}
	if s.ingest == nil {
		return status.Error(codes.FailedPrecondition, "this control plane does not ingest flows")
	}
	log := s.log.With("workload_id", id)
	var accepted uint64
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return stream.SendAndClose(&innerwallv1.ReportFlowsResponse{AcceptedRecords: accepted})
		}
		if err != nil {
			return err
		}
		res, err := s.ingest.Ingest(ctx, id, req)
		switch {
		case errors.Is(err, ingest.ErrInvalidWindow):
			return status.Error(codes.InvalidArgument, err.Error())
		case errors.Is(err, registry.ErrWorkloadUnknown):
			return status.Error(codes.PermissionDenied, err.Error())
		case err != nil:
			if ctx.Err() != nil {
				return nil
			}
			log.Error("ingesting flow window", "error", err)
			return status.Error(codes.Internal, "ingesting flow window")
		}
		accepted += uint64(res.Accepted) //nolint:gosec // non-negative
		log.Debug("flow window ingested", "window_start", req.GetWindowStart().AsTime(), "records", res.Accepted, "rejected", res.Rejected)
	}
}
