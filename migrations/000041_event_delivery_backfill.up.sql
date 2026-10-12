INSERT INTO event_deliveries(event_id,consumer_id,status,attempt_count,max_attempts,available_at,finished_at)
SELECT id,'activity.v1',status,attempt_count,max_attempts,available_at,COALESCE(delivered_at,UTC_TIMESTAMP(6))
FROM outbox_events WHERE status IN ('DELIVERED','FAILED')
UNION ALL
SELECT id,'notifications.v1','SKIPPED',0,max_attempts,available_at,UTC_TIMESTAMP(6)
FROM outbox_events WHERE status IN ('DELIVERED','FAILED') AND event_type IN ('incident.assigned','work.comment.added','work.work_note.added','incident.state.changed','incident.reopened','incident.resolved','incident.closed','incident.cancelled');
