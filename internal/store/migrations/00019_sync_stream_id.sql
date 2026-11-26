-- The stream a workload's sync state describes (issue #109).
--
-- A sync stream's end is recorded as the workload's offline mark, written
-- by the replica that served the stream after the stream has gone. An
-- agent whose stream ends redials at once when it was directed to, and
-- through a load balancer its next stream can open on another replica,
-- or on the incoming process of a deploy while the outgoing one is still
-- writing the marks of the streams it ended. Nothing orders one replica's
-- writes against another's, so the mark of the stream that ended could
-- land after the newer stream had recorded PENDING and SYNCED, leaving a
-- workload recorded offline while its stream was live until its next
-- acknowledgement.
--
-- sync_stream_id is a random identifier the serving replica gives each
-- sync stream when it registers it, written in the same statement that
-- records the stream open (sync state PENDING), so a stream registering
-- replaces it atomically. The offline mark is written only while it
-- still names the ending stream: when it names another, a newer stream
-- has opened since, and the mark describes a stream the record no longer
-- does. It says which stream of the workload registered last. It does
-- not say whether any stream is alive and nothing reads it to decide
-- that; it is one more per-stream fact beside the presented credential
-- of migration 00018 (ADR-0018 as amended).
--
-- NULL until the first stream registers after this migration. A NULL
-- names no stream, so nothing newer is known and an ending stream's
-- offline mark is written exactly as before.

-- +goose Up
ALTER TABLE workloads
    ADD COLUMN sync_stream_id uuid;

-- +goose Down
ALTER TABLE workloads
    DROP COLUMN sync_stream_id;
